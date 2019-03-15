package printer

import (
	"bytes"
	"testing"

	"vgitlab01.tq-net.de/tq-em/tools/dbus-codegen-go.git/token"
)

func TestPrint(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := Print(&buf, []*token.Interface{
		{
			Name:       "foo.org",
			Methods:    []*token.Method{},
			Properties: []*token.Property{},
			Signals:    []*token.Signal{},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// TODO: test something
}

func TestIfaceName(t *testing.T) {
	p := &printer{}
	for name, want := range map[string]string{
		"camel_case_name": "Camel_Case_Name",
	} {
		if have := p.ifaceType(&token.Interface{
			Name: name,
		}); have != want {
			t.Errorf("ifaceType(%q) = %q, want %q", name, have, want)
		}
	}
}
