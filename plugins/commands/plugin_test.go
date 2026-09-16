package commands

import "testing"

func TestEncodeBrand(t *testing.T) {
	got := encodeBrand("Gate")
	want := []byte{4, 'G', 'a', 't', 'e'}
	if string(got) != string(want) {
		t.Fatalf("encodeBrand = %v, want %v", got, want)
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"secret", "other"}, "secret") {
		t.Fatal("configured password was not found")
	}
	if contains([]string{"secret"}, "SECRET") {
		t.Fatal("password matching must remain exact")
	}
}
