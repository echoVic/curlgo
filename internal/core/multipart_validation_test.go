package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidateMultipartFiles(t *testing.T) {
	path := uploadFile(t, "upload.bin", []byte("unchanged"))
	file := FormPart{Kind: FormFile, Name: "file", FilePath: path}
	for _, tc := range []struct {
		name  string
		parts []FormPart
		valid bool
	}{
		{"empty", nil, true},
		{"text", []FormPart{{Kind: FormText, Name: "text", Value: "value"}}, true},
		{"regular", []FormPart{file, file}, true},
		{"missing", []FormPart{{Kind: FormFile, Name: "file", FilePath: filepath.Join(t.TempDir(), "missing")}}, false},
		{"directory", []FormPart{{Kind: FormFile, Name: "file", FilePath: t.TempDir()}}, false},
		{"nonregular", []FormPart{{Kind: FormFile, Name: "file", FilePath: os.DevNull}}, false},
		{"partialOpenFailure", []FormPart{file, {Kind: FormFile, Name: "file", FilePath: filepath.Join(t.TempDir(), "missing")}}, false},
		{"invalidName", []FormPart{{Kind: FormText, Name: "bad\nname"}}, false},
		{"invalidKind", []FormPart{{Kind: "unknown", Name: "field"}}, false},
		{"relativePath", []FormPart{{Kind: FormFile, Name: "file", FilePath: "relative"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]FormPart(nil), tc.parts...)
			err := ValidateMultipartFiles(tc.parts)
			if (err == nil) != tc.valid || !reflect.DeepEqual(tc.parts, before) {
				t.Fatalf("err=%v, parts=%+v", err, tc.parts)
			}
		})
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("validation changed upload: %q, %v", data, err)
	}
	if posixPermissions() && os.Geteuid() != 0 {
		if err := os.Chmod(path, 0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0600) })
		if err := ValidateMultipartFiles([]FormPart{file}); err == nil {
			t.Fatal("unopenable file accepted")
		}
	}
}

func TestValidateMultipartFilesDoesNotBufferUploads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1 << 30); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMultipartFiles([]FormPart{{Kind: FormFile, Name: "file", FilePath: path}}); err != nil {
		t.Fatalf("sparse upload above body/input limits rejected: %v", err)
	}
}
