package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git.emile.space/r2wars-web/internal/model"
)

func TestParseDSLInline(t *testing.T) {
	in := `# a sample battle
battle "demo"
arena 4096
rounds 200
gap 64

bot "warrior1" arch x86 bits 32 {
    mov eax, 1
    nop
    jmp 0
}

bot "warrior2" arch x86 bits 32 {
    nop
}
`
	spec, err := ParseDSL(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if spec.Name != "demo" {
		t.Errorf("name = %q, want demo", spec.Name)
	}
	if spec.Config.ArenaSize != 4096 || spec.Config.MaxRounds != 200 || spec.Config.PlacementGap != 64 {
		t.Errorf("config = %+v", spec.Config)
	}
	if len(spec.Bots) != 2 {
		t.Fatalf("got %d bots, want 2", len(spec.Bots))
	}
	if spec.Bots[0].Name != "warrior1" || spec.Bots[0].Arch != "x86" || spec.Bots[0].Bits != "32" {
		t.Errorf("bot0 header = %+v", spec.Bots[0])
	}
	wantSrc := "mov eax, 1\nnop\njmp 0"
	if spec.Bots[0].Source != wantSrc {
		t.Errorf("bot0 source = %q, want %q", spec.Bots[0].Source, wantSrc)
	}
}

func TestRoundTrip(t *testing.T) {
	spec := Spec{
		Name:   "rt",
		Config: Config{ArenaSize: 8192, MaxRounds: 50, PlacementGap: 128},
		Bots: []BotSpec{
			{Name: "a", Arch: "x86", Bits: "32", Source: "mov eax, 1\nnop"},
			{Name: "b", Arch: "x86", Bits: "64", Source: "nop\nnop"},
		},
	}
	got, err := ParseDSL(strings.NewReader(FormatDSL(spec)))
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if !reflect.DeepEqual(got, spec) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, spec)
	}
}

func TestParseDSLSourceFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w.asm")
	if err := os.WriteFile(path, []byte("mov eax, 2\nnop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := "battle \"f\"\narena 1024\nrounds 10\nbot \"filebot\" arch x86 bits 32 source \"" + path + "\"\n"
	spec, err := ParseDSL(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if len(spec.Bots) != 1 {
		t.Fatalf("want 1 bot, got %d", len(spec.Bots))
	}
	if !strings.Contains(spec.Bots[0].Source, "mov eax, 2") {
		t.Errorf("file source not loaded: %q", spec.Bots[0].Source)
	}
}

func TestParseDSLSampleFile(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "sample.dsl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	spec, err := ParseDSL(f)
	if err != nil {
		t.Fatalf("ParseDSL(sample.dsl): %v", err)
	}
	if spec.Name != "sample" || len(spec.Bots) != 2 {
		t.Fatalf("spec = %+v", spec)
	}
	if !strings.Contains(spec.Bots[1].Source, "pop esp") {
		t.Errorf("real bot source not loaded: %q", spec.Bots[1].Source)
	}
}

func TestParseDSLErrors(t *testing.T) {
	cases := map[string]string{
		"unterminated block": "bot \"x\" arch x86 bits 32 {\n    nop\n",
		"missing arch":       "bot \"x\" bits 32 {\n}\n",
		"missing bits":       "bot \"x\" arch x86 {\n}\n",
		"bad arena":          "arena notanumber\n",
		"unknown directive":  "frobnicate 3\n",
		"bad bot token":      "bot \"x\" arch x86 bits 32 wat {\n}\n",
		"no source at all":   "bot \"x\" arch x86 bits 32\n",
	}
	for name, in := range cases {
		if _, err := ParseDSL(strings.NewReader(in)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestSpecFromModel(t *testing.T) {
	b := model.Battle{
		Name:      "modelbattle",
		ArenaSize: 2048,
		MaxRounds: 33,
		Bots: []model.Bot{
			{
				ID: 1, Name: "m1", Source: "nop",
				Archs: []model.Arch{{Name: "x86"}},
				Bits:  []model.Bit{{Name: "32"}},
			},
		},
	}
	spec, err := SpecFromModel(b)
	if err != nil {
		t.Fatalf("SpecFromModel: %v", err)
	}
	if spec.Name != "modelbattle" || spec.Config.ArenaSize != 2048 || spec.Config.MaxRounds != 33 {
		t.Errorf("spec = %+v", spec)
	}
	if len(spec.Bots) != 1 || spec.Bots[0].Arch != "x86" || spec.Bots[0].Bits != "32" {
		t.Errorf("bots = %+v", spec.Bots)
	}

	// Missing arch should error.
	bad := model.Battle{Bots: []model.Bot{{Name: "x", Bits: []model.Bit{{Name: "32"}}}}}
	if _, err := SpecFromModel(bad); err == nil {
		t.Error("expected error for bot with no arch")
	}
}
