package library

import (
	"archive/zip"
	"bytes"

	"math/rand/v2"
	"os"
	"path/filepath"

	"testing"
)

// pptx is a PowerPoint file as PowerPoint saves one: a zip of the
// presentation's XML parts and its media, which are already compressed
// (the pictures in a template), so the file is about as big as its media.
func pptx(t *testing.T, media int) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	part := func(name string, data []byte, method uint16) {
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	part("[Content_Types].xml", []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="png" ContentType="image/png"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/></Types>`), zip.Deflate)
	part("ppt/presentation.xml", []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/>`), zip.Deflate)
	img := make([]byte, media)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img {
		img[i] = byte(r.Uint32())
	}
	part("ppt/media/image1.png", img, zip.Store)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The same template on this computer: every agent gets it whole, the one
// linked to the library, one given copies, and agents in a WSL distro,
// which always get copies.
func TestSkillTemplateReachesEveryAgent(t *testing.T) {
	h := wslSandbox(t)
	src := filepath.Join(home(), "src", "skills")
	skill(t, filepath.Join(src, "pdf"), "pdf", "Read PDFs")
	deck := pptx(t, 6<<20)
	write(t, filepath.Join(src, "pdf/templates/report.pptx"), string(deck))
	ok(t)(SetSkillHow("codex", HowCopy))
	ok(t)(InstallSkills(src, []string{"pdf"}, []string{"claude", "codex", wslCodex, wslClaude, wslPi}))
	for _, p := range []string{
		filepath.Join(home(), ".claude/skills/pdf"), filepath.Join(home(), ".codex/skills/pdf"),
		filepath.Join(h, ".codex/skills/pdf"), filepath.Join(h, ".claude/skills/pdf"), filepath.Join(h, ".pi/agent/skills/pdf"),
	} {
		if got, err := os.ReadFile(filepath.Join(p, "templates/report.pptx")); err != nil || !bytes.Equal(got, deck) {
			t.Errorf("%s: %d bytes of %d, %v", p, len(got), len(deck), err)
		}
	}
}
