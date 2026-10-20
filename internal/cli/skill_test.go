package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fvdsn/jig/internal/jig"
)

func TestSkillPrintsTheEmbeddedSkill(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	jig.EmbeddedSkill = os.DirFS(abs)
	t.Cleanup(func() { jig.EmbeddedSkill = nil })
	var out bytes.Buffer
	if err := Run([]string{"skill"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "---\nname: jig\n") {
		t.Fatalf("skill output:\n%.80s", out.String())
	}
	if err := Run([]string{"skill", "extra"}, &out); err == nil {
		t.Fatal("extra argument accepted")
	}
}
