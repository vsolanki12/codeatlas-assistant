package atlas

import "testing"

func TestClientBinaryPath(t *testing.T) {
	t.Setenv("CODEATLAS_BIN", "/env/atlas")
	if got := (&Client{}).binaryPath(); got != "/env/atlas" {
		t.Fatalf("default binary = %q, want environment override", got)
	}
	if got := (&Client{Binary: "/flag/atlas"}).binaryPath(); got != "/flag/atlas" {
		t.Fatalf("explicit binary = %q, want flag value", got)
	}
}

func TestClientBinaryPathFallsBackToPATH(t *testing.T) {
	t.Setenv("CODEATLAS_BIN", "")
	if got := (&Client{}).binaryPath(); got != "atlas" {
		t.Fatalf("fallback binary = %q, want atlas", got)
	}
}
