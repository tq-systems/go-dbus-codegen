package printer

import (
	"bytes"
	"errors"
	goformat "go/format"
	goparser "go/parser"
	gotoken "go/token"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/tq-systems/go-dbus-codegen/token"
)

// PrintOption is a Print configuration option.
type PrintOption func(p *printer)

type printer struct {
	pkgName  string
	gofmt    bool
	prefixes []string
}

// WithPackageName overrides the package name of generated code.
func WithPackageName(name string) PrintOption {
	return func(p *printer) {
		p.pkgName = name
	}
}

// WithGofmt gofmts generated code, enabled by default.
//
// Not recommended to disable it, only for debugging the output,
// because gofmt works as a validation step as well.
func WithGofmt(enable bool) PrintOption {
	return func(p *printer) {
		p.gofmt = enable
	}
}

// WithPrefixes prefixes to strip from interface names, be careful
// when using it may lead to compilation errors.
func WithPrefixes(prefixes []string) PrintOption {
	return func(p *printer) {
		p.prefixes = prefixes
	}
}

var identRegexp = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9_]*$")

const srcTemplate = `// Code generated by dbus-codegen-go. DO NOT EDIT.
//
{{- range $iface := .Interfaces }}
// {{ $iface.Name }}
{{- if $iface.Methods }}
//   Methods
{{- range $method := $iface.Methods }}
//     {{ $method.Name }}
{{- end }}
{{- end }}
{{- if $iface.Properties }}
//   Properties
{{- range $prop := $iface.Properties }}
//     {{ $prop.Name }}
{{- end }}
{{- end }}
{{- if $iface.Signals }}
//   Signals
{{- range $signal := $iface.Signals }}
//     {{ $signal.Name }}
{{- end }}
{{- end }}
//
{{- end }}
package {{ .PackageName }}

import (
	"log"

	"github.com/godbus/dbus"
)

const (
	methodPropertyGet = "org.freedesktop.DBus.Properties.Get"
	methodPropertySet = "org.freedesktop.DBus.Properties.Set"
)

// Avoid error caused by unused log import
var _ = log.Printf

// Interface is a DBus interface implementation.
type Interface interface {
	iface() string
}

// LookupInterface returns an interface for the named object.
func LookupInterface(object dbus.BusObject, iface string) Interface {
	switch iface {
{{- range $iface := .Interfaces }}
	case {{ ifaceNameConst $iface }}:
		return New{{ ifaceType $iface }}(object)
{{- end }}
	default:
		return nil
	}
}

// Signal is a common interface for all signals.
type Signal interface {
	Name()      string
	Interface() string
	Sender()    string
	Path()      dbus.ObjectPath
}

// LookupSignal converts the given raw DBus signal into typed one or returns nil.
func LookupSignal(signal *dbus.Signal) Signal {
	switch signal.Name {
{{- range $iface := .Interfaces }}
{{- range $signal := $iface.Signals }}
	case {{ ifaceNameConst $iface }} + "." + "{{ $signal.Name }}":
{{- range $i, $argument := $signal.Args }}
		v{{ $i }}, ok := signal.Body[{{ $i }}].({{ $argument.Type }})
		if !ok {
			log.Printf("[{{ $.PackageName }}] {{ argName $argument "v" $i true }} is %T, not {{ $argument.Type }}", signal.Body[{{ $i }}])
		}
{{- end }}
		return &{{ signalType $iface $signal }}{
			sender: signal.Sender,
			path:   signal.Path,
			Body: {{ signalBodyType $iface $signal }}{
{{- range $i, $argument := $signal.Args }}
				{{ argName $argument "v" $i true }}: v{{ $i }},
{{- end }}
			},
		}
{{- end }}
{{- end }}
	default:
		return nil
	}
}

// AddMatchRule returns AddMatch rule for the given signal. 
func AddMatchRule(sig Signal) string {
	return "type='signal',interface='" + sig.Interface() + "',member='" + sig.Name() + "'"
}

// Interface name constants.
const (
{{- range $iface := .Interfaces }}
	{{ ifaceNameConst $iface }} = "{{ $iface.Name }}"
{{- end }}
)
{{- define "annotations" }}
{{- range $annotation := .Annotations -}}
// @{{ $annotation.Name }} = {{ $annotation.Value }}
{{- end }}
{{- end }}
{{ range $iface := .Interfaces }}
// {{ ifaceNewType $iface }} creates and allocates {{ $iface.Name }}.
func {{ ifaceNewType $iface }}(object dbus.BusObject) *{{ ifaceType $iface }} {
	return &{{ ifaceType $iface }}{object}
}

// {{ ifaceType $iface }} implements {{ $iface.Name }} D-Bus interface.
{{- template "annotations" $iface }}
type {{ ifaceType $iface }} struct {
	object dbus.BusObject
}

// iface implements the Interface interface.
func (o *{{ ifaceType $iface }}) iface() string {
	return {{ ifaceNameConst $iface }}
}
{{ range $method := $iface.Methods }}
// {{ methodType $method }} calls {{ $iface.Name }}.{{ $method.Name }} method.
{{- template "annotations" $method }}
func (o *{{ ifaceType $iface }}) {{ methodType $method }}({{ joinMethodInArgs $method }}) ({{ joinMethodOutArgs $method }}err error) {
	err = o.object.Call({{ ifaceNameConst $iface }} + "." + "{{ $method.Name }}", 0, {{ joinArgNames $method.In }}).Store({{ joinStoreArgs $method.Out }})
	return
}
{{ end }}
{{- range $prop := $iface.Properties }}
{{- if propNeedsGet $iface $prop }}
// {{ propGetType $prop }} gets {{ $iface.Name }}.{{ $prop.Name }} property.
{{- template "annotations" $prop }}
func (o *{{ ifaceType $iface }}) {{ propGetType $prop }}() ({{ propArgName $prop }} {{ $prop.Arg.Type }}, err error) {
	err = o.object.Call(methodPropertyGet, 0, {{ ifaceNameConst $iface }}, "{{ $prop.Name }}").Store(&{{ propArgName $prop }})
	return
}
{{- end }}
{{- if propNeedsSet $iface $prop }}
// {{ propSetType $prop }} sets {{ $iface.Name }}.{{ $prop.Name }} property.
{{- template "annotations" $prop }}
func (o *{{ ifaceType $iface }}) {{ propSetType $prop }}({{ propArgName $prop }} {{ $prop.Arg.Type }}) error {
	return o.object.Call(methodPropertySet, 0, {{ ifaceNameConst $iface }}, "{{ $prop.Name }}", {{ propArgName $prop }}).Store()
}
{{- end }}
{{ end }}
{{ range $signal := $iface.Signals }}
// {{ signalType $iface $signal }} represents {{ $iface.Name }}.{{ $signal.Name }} signal.
{{- template "annotations" $signal }}
type {{ signalType $iface $signal }} struct {
	sender string
	path   dbus.ObjectPath
	Body   {{ signalBodyType $iface $signal }}
}

// Name returns the signal's name.
func (s *{{ signalType $iface $signal }}) Name() string {
	return "{{ $signal.Name }}"
}

// Interface returns the signal's interface.
func (s *{{ signalType $iface $signal }}) Interface() string {
	return {{ ifaceNameConst $iface }}
}

// Sender returns the signal's sender unique name.
func (s *{{ signalType $iface $signal }}) Sender() string {
	return s.sender
}

// Path returns path that emitted the signal.
func (s *{{ signalType $iface $signal }}) Path() dbus.ObjectPath {
	return s.path
}

// {{ signalBodyType $iface $signal }} is body container.
type {{ signalBodyType $iface $signal }} struct {
	{{ joinSignalArgs $signal }}
}
{{ end }}
{{- end }}`

type tmplContext struct {
	PackageName string
	Interfaces  []*token.Interface
}

// Print generates code for the provided interfaces and writes it to out.
func Print(out io.Writer, ifaces []*token.Interface, opts ...PrintOption) error {
	p := &printer{
		pkgName: "dbusgen",
		gofmt:   true,
	}
	for _, opt := range opts {
		opt(p)
	}
	if !identRegexp.MatchString(p.pkgName) {
		return errors.New("package name is not valid")
	}
	if len(ifaces) == 0 {
		return errors.New("no interfaces given")
	}

	p.prepareIfaces(ifaces)
	tmpl := template.Must(template.New("main").Funcs(template.FuncMap{
		"ifaceNameConst":    p.ifaceNameConst,
		"ifaceNewType":      p.ifaceNewType,
		"ifaceType":         p.ifaceType,
		"methodType":        p.methodType,
		"propType":          p.propType,
		"propGetType":       p.propGetType,
		"propSetType":       p.propSetType,
		"propArgName":       p.propArgName,
		"propNeedsGet":      p.propNeedsGet,
		"propNeedsSet":      p.propNeedsSet,
		"signalType":        p.signalType,
		"signalBodyType":    p.signalBodyType,
		"argName":           p.argName,
		"joinMethodInArgs":  p.joinMethodInArgs,
		"joinMethodOutArgs": p.joinMethodOutArgs,
		"joinArgNames":      p.joinArgNames,
		"joinStoreArgs":     p.joinStoreArgs,
		"joinSignalArgs":    p.joinSignalArgs,
	}).Parse(srcTemplate))

	var buf bytes.Buffer
	var err error
	if err = tmpl.Execute(&buf, &tmplContext{
		PackageName: p.pkgName,
		Interfaces:  ifaces,
	}); err != nil {
		return err
	}
	fset := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fset, "", buf.Bytes(), goparser.ParseComments)
	if err != nil {
		// TODO: print helpful message
		// if _, ok := err.(scanner.ErrorList); ok {
		// 	return errors.New("unable to parse generated code")
		// }
		return err
	}
	if p.gofmt {
		return goformat.Node(out, fset, file)
	}
	_, err = out.Write(buf.Bytes())
	return err
}

// prepareIfaces sorts the given interfaces and all their nested entities.
func (p *printer) prepareIfaces(ifaces []*token.Interface) {
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].Name < ifaces[j].Name
	})
	for _, iface := range ifaces {
		sort.Slice(iface.Methods, func(i, j int) bool {
			return iface.Methods[i].Name < iface.Methods[j].Name
		})
		sort.Slice(iface.Properties, func(i, j int) bool {
			return iface.Properties[i].Name < iface.Properties[j].Name
		})
		sort.Slice(iface.Signals, func(i, j int) bool {
			return iface.Signals[i].Name < iface.Signals[j].Name
		})
	}
}

func isKeyword(s string) bool {
	// TODO: validate it doesn't match imported package names
	return gotoken.Lookup(s).IsKeyword()
}

var ifaceRegexp = regexp.MustCompile(`[._][a-zA-Z0-9]`)

func (p *printer) ifaceType(iface *token.Interface) string {
	name := iface.Name
	for _, prefix := range p.prefixes {
		if prefix[len(prefix)-1] == '.' {
			prefix = prefix[:len(prefix)-1]
		}
		if i := strings.Index(name, prefix); i != -1 {
			name = name[i+len(prefix)+1:]
			break
		}
	}
	name = strings.Title(name)
	if isKeyword(name) {
		return name
	}
	return ifaceRegexp.ReplaceAllStringFunc(name, func(s string) string {
		return "_" + strings.ToUpper(s[1:])
	})
}

func (p *printer) ifaceNewType(iface *token.Interface) string {
	return "New" + p.ifaceType(iface)
}

func (p *printer) ifaceNameConst(iface *token.Interface) string {
	return "Interface" + p.ifaceType(iface)
}

func (p *printer) ifaceHasMethod(iface *token.Interface, name string) bool {
	for _, method := range iface.Methods {
		if method.Name == name {
			return true
		}
	}
	return false
}

func (p *printer) ifacesHaveSignals(ifaces []*token.Interface) bool {
	for _, iface := range ifaces {
		if len(iface.Signals) != 0 {
			return true
		}
	}
	return false
}

func (p *printer) methodType(method *token.Method) string {
	return strings.Title(method.Name)
}

func (p *printer) propType(prop *token.Property) string {
	return strings.Title(prop.Name)
}

func (p *printer) propGetType(prop *token.Property) string {
	return "Get" + p.propType(prop)
}

func (p *printer) propSetType(prop *token.Property) string {
	return "Set" + p.propType(prop)
}

func (p *printer) propNeedsSet(iface *token.Interface, prop *token.Property) bool {
	if !prop.Write {
		return false
	}
	return p.propNeedsAccessor(iface, p.propSetType(prop))
}

func (p *printer) propNeedsGet(iface *token.Interface, prop *token.Property) bool {
	if !prop.Read {
		return false
	}
	return p.propNeedsAccessor(iface, p.propGetType(prop))
}

func (p *printer) propNeedsAccessor(iface *token.Interface, name string) bool {
	for _, method := range iface.Methods {
		if method.Name == name {
			return false
		}
	}
	return true
}

func (p *printer) signalType(iface *token.Interface, signal *token.Signal) string {
	return p.ifaceType(iface) + "_" + strings.Title(signal.Name) + "Signal"
}

func (p *printer) signalBodyType(iface *token.Interface, signal *token.Signal) string {
	return p.signalType(iface, signal) + "Body"
}

var varRegexp = regexp.MustCompile("_+[a-zA-Z0-9]")

func (p *printer) argName(arg *token.Arg, prefix string, i int, export bool) string {
	name := arg.Name
	if name == "" {
		name = prefix + strconv.Itoa(i)
	} else {
		name = strings.ToLower(name[:1]) +
			varRegexp.ReplaceAllStringFunc(name[1:], func(s string) string {
				return strings.Title(strings.TrimLeft(s, "_"))
			})
	}
	if export {
		name = strings.Title(name)
	}
	if isKeyword(name) {
		return prefix + strings.Title(name)
	}
	return name
}

func (p *printer) propArgName(prop *token.Property) string {
	return p.argName(prop.Arg, "v", 0, false)
}

func (p *printer) joinStoreArgs(args []*token.Arg) string {
	var buf strings.Builder
	for i := range args {
		if i != 0 {
			buf.WriteByte(',')
		}
		buf.WriteByte('&')
		buf.WriteString(p.argName(args[i], "out", i, false))
	}
	return buf.String()
}

func (p *printer) joinArgs(args []*token.Arg, separator byte, suffix string, export bool) string {
	var buf strings.Builder
	for i := range args {
		buf.WriteString(p.argName(args[i], suffix, i, export))
		buf.WriteByte(' ')
		buf.WriteString(args[i].Type)
		buf.WriteByte(separator)
	}
	return buf.String()
}

func (p *printer) joinSignalArgs(sig *token.Signal) string {
	return p.joinArgs(sig.Args, ';', "v", true)
}

func (p *printer) joinMethodInArgs(method *token.Method) string {
	return p.joinArgs(method.In, ',', "in", false)
}

func (p *printer) joinMethodOutArgs(method *token.Method) string {
	return p.joinArgs(method.Out, ',', "out", false)
}

func (p *printer) joinArgNames(args []*token.Arg) string {
	var buf strings.Builder
	for i := range args {
		if i != 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(p.argName(args[i], "in", i, false))
	}
	return buf.String()
}
