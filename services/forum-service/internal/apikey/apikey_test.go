package apikey

import "testing"

// Hash SHA-256 de "clave-de-prueba-grupo4" (el de .env.example).
const hashAgencia = "01bcc01f34c9f358394fca4be4af4a2fbb80a0122fe52bf7bf120b2d29b99e1d"

func TestLaClaveSeReconocePorSuHash(t *testing.T) {
	keys, err := Parse("agencia_de_viajes:" + hashAgencia)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := keys.Lookup("clave-de-prueba-grupo4"); !ok || c != "agencia_de_viajes" {
		t.Errorf("Lookup = %q, %v; se esperaba agencia_de_viajes", c, ok)
	}
	for _, mala := range []string{"", "otra", hashAgencia} {
		if _, ok := keys.Lookup(mala); ok {
			t.Errorf("se aceptó la clave %q", mala)
		}
	}
}

func TestConfiguracionInvalida(t *testing.T) {
	for _, cfg := range []string{"agencia_de_viajes", "agencia_de_viajes:no-es-hex", ":" + hashAgencia, "agencia_de_viajes:" + hashAgencia + ",agencia_de_viajes:" + hashAgencia} {
		if _, err := Parse(cfg); err == nil {
			t.Errorf("se aceptó la configuración %q", cfg)
		}
	}
	if keys, err := Parse(""); err != nil || keys.Len() != 0 {
		t.Errorf("configuración vacía: %v", err)
	}
}
