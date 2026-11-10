# Despliegue en Azure

Estudiario corre en una máquina virtual de Azure con el mismo `docker compose` que en local, más [Caddy](https://caddyserver.com) adelante como única puerta de entrada con HTTPS automático.

| Archivo | Para qué |
|---|---|
| `setup.sh` | Prepara la VM (swap, Docker), clona el repo, genera un `.env` con secretos nuevos y levanta todo |
| `docker-compose.cloud.yml` | Agrega Caddy y cierra los puertos de los demás servicios |
| `Caddyfile` | `/v1/*` y `/health` van a forum-service (capacidad para el Grupo 4); el resto, al frontend |

**Despliegue actual:** https://estudiario-ucc.chilecentral.cloudapp.azure.com (VM `estudiario-vm`, región Chile Central).

## 1. Crear la VM (una sola vez)

1. Activá **Azure for Students** en https://azure.microsoft.com/es-es/free/students con el mail de la UCC (da USD 100 de crédito, sin tarjeta).
2. En el portal: **Máquinas virtuales → Crear → Máquina virtual de Azure**.
   - **Imagen:** Ubuntu Server 24.04 LTS (x64).
   - **Tamaño:** `Standard_B2s` (2 vCPU, 4 GB). Cuesta unos USD 30 por mes del crédito.
   - **Región:** la más cercana que la deje crear (por ejemplo *Brazil South* o *East US*).
   - **Autenticación:** clave pública SSH; descargá el archivo `.pem` que te ofrece.
   - **Puertos de entrada:** marcá **SSH (22), HTTP (80) y HTTPS (443)**. Ningún otro.
   - **Disco:** 30 GB o más.
3. Cuando termine, entrá a la VM → **Información general → Nombre DNS → Configurar**, poné una etiqueta (por ejemplo `estudiario`) y guardá. El dominio queda como `estudiario.<región>.cloudapp.azure.com`.

## 2. Levantar Estudiario

Desde tu compu, entrá por SSH (el usuario es el que elegiste al crear la VM, por defecto `azureuser`):

```bash
ssh -i ruta/a/tu-clave.pem azureuser@estudiario.<región>.cloudapp.azure.com
```

Ya adentro de la VM:

```bash
curl -fsSL https://raw.githubusercontent.com/MichiSil/ProyectoEstudiario/main/deploy/azure/setup.sh -o setup.sh
bash setup.sh
```

El script pregunta el dominio, el mail del administrador del foro, la API key de Groq (opcional) y la del Club de Beneficios (opcional), y arma el `.env` a partir de `.env.example`:

- Genera contraseñas y secretos nuevos al azar para todas las bases, RabbitMQ, MinIO, Grafana, el JWT y el token interno. Ningún valor de desarrollo queda en la nube.
- `APP_URL` pasa a ser `https://<dominio>`: es el link que reciben por mail los autores de la agencia.
- **API key del Grupo 4.** Genera una clave nueva, guarda en el `.env` sólo su hash (`FORUM_API_KEYS=agencia_de_viajes:<sha256>`) y deja la clave en `~/clave-grupo4.txt`. Pasásela por privado a quien se la entrega al Grupo 4 y después borrá el archivo (`rm ~/clave-grupo4.txt`). La clave de prueba de `.env.example` no sirve en la nube.
- `CLUB_API_KEY` es la que entregó el Grupo 1 (la tiene Isabela). Si la dejás vacía, el perfil muestra que el Club no está disponible y los puntos quedan pendientes; se puede cargar después en el `.env` y reiniciar users-service.

La primera vez tarda unos 10 minutos en compilar las imágenes. Al terminar, la app queda en `https://estudiario.<región>.cloudapp.azure.com`.

## 3. Actualizar después de un merge a main

Volvé a correr el script dentro de la VM. Actualiza el código y reconstruye sin perder los datos. Al `.env` sólo le agrega las variables nuevas de `.env.example`, con secretos al azar, y cambia las que hayan quedado con el valor de desarrollo; si el consumidor del Grupo 4 todavía se llamaba `grupo4`, lo renombra a `agencia_de_viajes` y su clave sigue andando:

```bash
bash setup.sh
```

## Operación

```bash
cd ~/ProyectoEstudiario
C="sudo docker compose -f docker-compose.yml -f deploy/azure/docker-compose.cloud.yml"
$C ps                        # estado de cada servicio
$C logs -f planner-service   # logs de un servicio
$C restart planner-service   # reiniciar uno (por ejemplo, después de cambiar el .env)
```

**Paneles internos.** Jaeger, la consola de MinIO y Solr no se publican en internet. Para ver uno (por ejemplo Jaeger) desde tu compu:

```bash
# 1. En la VM: exponer Jaeger sólo en la propia VM (127.0.0.1), mientras haga falta
sudo docker run --rm -d --name tunel-jaeger --network estudiario_default \
  -p 127.0.0.1:16686:16686 alpine/socat tcp-listen:16686,fork,reuseaddr tcp:jaeger:16686

# 2. En tu compu: túnel SSH y abrir http://localhost:16686
ssh -i ruta/a/tu-clave.pem -L 16686:localhost:16686 azureuser@estudiario.<región>.cloudapp.azure.com

# 3. Al terminar, en la VM
sudo docker stop tunel-jaeger
```

Lo mismo sirve para Grafana (`grafana:3000`, usuario y contraseña en `GRAFANA_ADMIN_USER` y `GRAFANA_ADMIN_PASSWORD` del `.env`), Prometheus (`prometheus:9090`), la consola de MinIO (`minio:9001`), Solr (`solr:8983`), la interfaz de RabbitMQ (`rabbitmq:15672`) o Mailpit (`mailpit:8025`), cambiando el nombre y el puerto. El `GET /metrics` de cada servicio tampoco se publica: sólo lo lee Prometheus dentro de la red de docker.

**Probar desde afuera** (desde cualquier compu, con la clave del Grupo 4 en `CLAVE`):

```bash
URL=https://estudiario-ucc.chilecentral.cloudapp.azure.com
curl -s -o /dev/null -w '%{http_code}
' $URL/health         # forum-service: 200
curl -s -o /dev/null -w '%{http_code}
' $URL/               # la app: 200
curl -s -H "X-API-Key: $CLAVE" "$URL/v1/documents?limit=1"    # capacidad del Grupo 4: 200
curl -s -o /dev/null -w '%{http_code}
' $URL/v1/documents    # sin clave: 401
curl -s -o x.pdf -w '%{http_code}
' $URL/v1/public/documents/<id>/file   # PDF público de un documento de la agencia: 200
```

## Limitaciones conocidas

- **Los mails no salen a internet.** En la nube los mails (resumen publicado o rechazado, cuenta nueva de un autor de la agencia) los recibe Mailpit, igual que en local: se ven por el túnel de arriba (`mailpit:8025`). Mandarlos de verdad requiere un proveedor SMTP con dominio verificado y credenciales, que no tenemos.
- **Una sola VM.** Todo corre en una máquina: si se cae, se cae todo. Las dos instancias de forum-service protegen de la caída de un contenedor, no de la máquina.
- **Grafana, Prometheus y Jaeger sólo por túnel SSH.** No se publican para no exponer paneles internos.

**Apagar para ahorrar crédito.** En el portal, **Detener** la VM deja de cobrar el cómputo (el disco sigue costando unos centavos). Al volver a iniciarla, los contenedores arrancan solos.

**Secretos.** El `.env` vive sólo en la VM (`chmod 600`). No se sube a GitHub ni se comparte por el chat del grupo.
