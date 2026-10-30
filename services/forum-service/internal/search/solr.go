// Package search es la búsqueda de resúmenes aprobados sobre Apache Solr
// (ADR-006): el cliente HTTP del core `summaries`, la búsqueda que usa el
// foro y la capacidad /v1, y el indexador que mantiene el índice igual a
// MongoDB, que es la fuente de verdad (ADR-003).
//
// Como en la CLASE_5, a Solr se le habla por HTTP con net/http, sin librerías.
package search

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// Tiempos del ADR-010: una búsqueda tiene 2 s; escribir en el índice, 5 s.
const (
	QueryTimeout = 2 * time.Second
	WriteTimeout = 5 * time.Second
)

// CommitWithin es cuánto tarda Solr, como máximo, en hacer visible una
// escritura (ADR-006, 3): no se fuerza un commit por cada documento.
const CommitWithin = 5 * time.Second

//go:embed schema.json
var schemaJSON []byte

// Solr es el cliente del core summaries. baseURL es la URL del core, por
// ejemplo http://solr:8983/solr/summaries.
type Solr struct {
	baseURL string
	http    *http.Client
}

func NewSolr(baseURL string) *Solr {
	return &Solr{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

// Doc es lo que se indexa de un resumen aprobado: sólo lo que sirve para
// buscar, filtrar u ordenar (ADR-006, 1). Ni el PDF ni los datos del autor.
type Doc struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	CategoryText string    `json:"category_text"`
	CategoryKey  string    `json:"categoryKey"`
	TagKeys      []string  `json:"tagKeys"`
	Source       string    `json:"source"`
	RatingAvg    float64   `json:"ratingAvg"`
	RatingCount  int       `json:"ratingCount"`
	PublishedAt  time.Time `json:"publishedAt"`
}

// ToDoc arma el documento del índice a partir del resumen de MongoDB.
func ToDoc(s model.Summary) Doc {
	d := Doc{
		ID: s.ID, Title: s.Title, Description: s.Description,
		CategoryText: s.Category, CategoryKey: s.CategoryKey, TagKeys: s.TagKeys,
		Source: string(s.Source), RatingAvg: s.Rating.Average, RatingCount: s.Rating.Count,
		PublishedAt: s.CreatedAt.UTC(),
	}
	if s.PublishedAt != nil {
		d.PublishedAt = s.PublishedAt.UTC()
	}
	if d.TagKeys == nil {
		d.TagKeys = []string{}
	}
	return d
}

// ---------- Esquema ----------

// EnsureSchema aplica el esquema versionado (schema.json) con la Schema API:
// agrega lo que falta y reemplaza lo que ya existe, así que es idempotente y
// un cambio de esquema se aplica solo al reiniciar. También apaga la creación
// automática de campos del configset _default, para que un campo con otro
// nombre no termine en el índice sin que nadie lo decida.
func (c *Solr) EnsureSchema(ctx context.Context) error {
	var schema struct {
		FieldTypes []map[string]any `json:"add-field-type"`
		Fields     []map[string]any `json:"add-field"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return fmt.Errorf("schema.json: %w", err)
	}
	types, err := c.names(ctx, "/schema/fieldtypes", "fieldTypes")
	if err != nil {
		return err
	}
	fields, err := c.names(ctx, "/schema/fields", "fields")
	if err != nil {
		return err
	}
	cmds := map[string][]map[string]any{}
	for _, t := range schema.FieldTypes {
		op := "add-field-type"
		if types[t["name"].(string)] {
			op = "replace-field-type"
		}
		cmds[op] = append(cmds[op], t)
	}
	for _, f := range schema.Fields {
		op := "add-field"
		if fields[f["name"].(string)] {
			op = "replace-field"
		}
		cmds[op] = append(cmds[op], f)
	}
	// Los tipos van antes que los campos que los usan.
	for _, op := range []string{"add-field-type", "replace-field-type", "add-field", "replace-field"} {
		if len(cmds[op]) == 0 {
			continue
		}
		if err := c.post(ctx, "/schema", map[string]any{op: cmds[op]}, WriteTimeout); err != nil {
			return fmt.Errorf("aplicar %s: %w", op, err)
		}
	}
	return c.post(ctx, "/config", map[string]any{
		"set-user-property": map[string]string{"update.autoCreateFields": "false"},
	}, WriteTimeout)
}

// names devuelve los nombres de los tipos o campos que ya tiene el esquema.
func (c *Solr) names(ctx context.Context, path, key string) (map[string]bool, error) {
	var out map[string]json.RawMessage
	if err := c.get(ctx, path, nil, &out, WriteTimeout); err != nil {
		return nil, err
	}
	var items []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out[key], &items); err != nil {
		return nil, fmt.Errorf("respuesta de %s: %w", path, err)
	}
	set := map[string]bool{}
	for _, item := range items {
		set[item.Name] = true
	}
	return set, nil
}

// Ping verifica que el core responda.
func (c *Solr) Ping(ctx context.Context) error {
	return c.get(ctx, "/admin/ping", nil, nil, QueryTimeout)
}

// ---------- Escritura ----------

// Upsert agrega o reemplaza documentos (Solr reemplaza por id).
func (c *Solr) Upsert(ctx context.Context, docs []Doc) error {
	if len(docs) == 0 {
		return nil
	}
	return c.post(ctx, "/update?commitWithin="+strconv.Itoa(int(CommitWithin.Milliseconds())), docs, WriteTimeout)
}

// Delete borra documentos por id. Borrar uno que no está no es un error.
func (c *Solr) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.post(ctx, "/update?commitWithin="+strconv.Itoa(int(CommitWithin.Milliseconds())), map[string]any{"delete": ids}, WriteTimeout)
}

// DeleteAll vacía el índice, para la reconstrucción completa.
func (c *Solr) DeleteAll(ctx context.Context) error {
	return c.post(ctx, "/update?commit=true", map[string]any{"delete": map[string]string{"query": "*:*"}}, WriteTimeout)
}

// Commit hace visible ya todo lo escrito (sólo para la reconstrucción).
func (c *Solr) Commit(ctx context.Context) error {
	return c.post(ctx, "/update?commit=true", map[string]any{"commit": map[string]any{}}, WriteTimeout)
}

// ---------- Lectura ----------

// Hit es un resultado: el id y lo que se compara en la reconciliación.
type Hit struct {
	ID          string  `json:"id"`
	RatingAvg   float64 `json:"ratingAvg"`
	RatingCount int     `json:"ratingCount"`
}

type selectResponse struct {
	Response struct {
		NumFound int   `json:"numFound"`
		Docs     []Hit `json:"docs"`
	} `json:"response"`
}

// Query arma los parámetros de Solr para una búsqueda (RF-19, RF-20):
//   - texto con edismax sobre el título (más peso), la categoría y la descripción;
//   - filtros con {!term}, que toma el valor literal: lo que escribe el usuario
//     nunca se interpreta como sintaxis de Solr (como en la CLASE_5);
//   - orden con desempate por id, para que las páginas sean estables.
func Query(q model.SearchQuery) url.Values {
	p := url.Values{
		"defType": {"edismax"},
		"q.alt":   {"*:*"},
		"qf":      {"title^5 category_text^2 description"},
		"mm":      {"100%"},
		"fl":      {"id,ratingAvg,ratingCount"},
		"start":   {strconv.Itoa((q.Page - 1) * q.Size)},
		"rows":    {strconv.Itoa(q.Size)},
	}
	if q.Text != "" {
		p.Set("q", q.Text)
	}
	if q.Category != "" {
		p.Add("fq", "{!term f=categoryKey}"+q.Category)
	}
	for _, t := range q.Tags {
		p.Add("fq", "{!term f=tagKeys}"+t)
	}
	if q.MinRating > 0 {
		p.Add("fq", "ratingAvg:["+strconv.FormatFloat(q.MinRating, 'f', -1, 64)+" TO *]")
	}
	switch {
	case q.Sort == model.SortRating:
		p.Set("sort", "ratingAvg desc, ratingCount desc, publishedAt desc, id asc")
	case q.Sort == model.SortRelevance && q.Text != "":
		p.Set("sort", "score desc, publishedAt desc, id asc")
	default: // date, o relevancia sin texto
		p.Set("sort", "publishedAt desc, id asc")
	}
	return p
}

// Select corre una búsqueda y devuelve los ids en orden y el total.
func (c *Solr) Select(ctx context.Context, params url.Values) ([]Hit, int, error) {
	var out selectResponse
	if err := c.get(ctx, "/select", params, &out, QueryTimeout); err != nil {
		return nil, 0, err
	}
	return out.Response.Docs, out.Response.NumFound, nil
}

// All recorre todo el índice (para la reconciliación), de a 1000 por id.
func (c *Solr) All(ctx context.Context) (map[string]Hit, error) {
	all := map[string]Hit{}
	cursor := "*"
	for {
		params := url.Values{
			"q": {"*:*"}, "fl": {"id,ratingAvg,ratingCount"}, "rows": {"1000"},
			"sort": {"id asc"}, "cursorMark": {cursor},
		}
		var out struct {
			selectResponse
			NextCursorMark string `json:"nextCursorMark"`
		}
		if err := c.get(ctx, "/select", params, &out, WriteTimeout); err != nil {
			return nil, err
		}
		for _, h := range out.Response.Docs {
			all[h.ID] = h
		}
		if out.NextCursorMark == cursor || out.NextCursorMark == "" {
			return all, nil
		}
		cursor = out.NextCursorMark
	}
}

// ---------- HTTP ----------

// ErrUnavailable: Solr no respondió a tiempo, no se pudo conectar o devolvió
// un error del servidor. La búsqueda lo informa como 503 SEARCH_UNAVAILABLE.
var ErrUnavailable = errors.New("Solr no está disponible")

func (c *Solr) get(ctx context.Context, path string, params url.Values, out any, timeout time.Duration) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("wt", "json")
	return c.do(ctx, http.MethodGet, path+"?"+params.Encode(), nil, out, timeout)
}

func (c *Solr) post(ctx context.Context, path string, body any, timeout time.Duration) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, bytes.NewReader(b), nil, timeout)
}

func (c *Solr) do(ctx context.Context, method, path string, body io.Reader, out any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return fmt.Errorf("Solr respondió HTTP %d en %s: %s", resp.StatusCode, path, msg)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
