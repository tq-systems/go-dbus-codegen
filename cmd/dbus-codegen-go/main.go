package main

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"
	"github.com/tq-systems/go-dbus-codegen/parser"
	"github.com/tq-systems/go-dbus-codegen/printer"
	"github.com/tq-systems/go-dbus-codegen/token"
)

var (
	destFlag     []string
	onlyFlag     []string
	exceptFlag   []string
	prefixesFlag []string
	systemFlag   bool
	packageFlag  string
	gofmtFlag    bool
	xmlFlag      bool
)

type stringsFlag []string

func (ss *stringsFlag) String() string {
	return "[" + strings.Join(*ss, ", ") + "]"
}

func (ss *stringsFlag) Set(arg string) error {
	for _, s := range strings.Split(arg, ",") {
		if s == "" {
			continue
		}
		*ss = append(*ss, s)
	}
	return nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [FLAG...] [PATH...]

Takes D-Bus Introspection Data Format and generates go code for it.

Flags:
`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Var((*stringsFlag)(&destFlag), "dest", "destination name(s) to introspect")
	flag.Var((*stringsFlag)(&onlyFlag), "only", "generate code only for the named interfaces")
	flag.Var((*stringsFlag)(&exceptFlag), "except", "skip the named interfaces")
	flag.Var((*stringsFlag)(&prefixesFlag), "prefix", "prefix to strip from interface names")
	flag.BoolVar(&systemFlag, "system", false, "connect to the system bus")
	flag.StringVar(&packageFlag, "package", "dbusgen", "generated package name")
	flag.BoolVar(&gofmtFlag, "gofmt", true, "gofmt results")
	flag.BoolVar(&xmlFlag, "xml", false, "combine the dest's introspections into a single document")
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	var ifaces []*token.Interface
	if len(destFlag) == 0 && xmlFlag {
		return errors.New("flag -xml cannot be used without -dest flag")
	}
	if len(destFlag) != 0 {
		if flag.NArg() > 0 {
			return errors.New("cannot combine arguments and -dest flag")
		}
		conn, err := connect(systemFlag)
		if err != nil {
			return err
		}
		defer conn.Close()

		if xmlFlag {
			b, err := generateXML(conn, destFlag)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		ifaces, err = parseDest(conn, destFlag)
		if err != nil {
			return err
		}
	} else if flag.NArg() > 0 {
		for _, filename := range flag.Args() {
			b, err := ioutil.ReadFile(filename)
			if err != nil {
				return err
			}
			chunk, err := parser.Parse(b)
			if err != nil {
				return err
			}
			ifaces = merge(ifaces, chunk)
		}
	} else {
		b, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		ifaces, err = parser.Parse(b)
		if err != nil {
			return err
		}
	}

	if len(onlyFlag) != 0 && len(exceptFlag) != 0 {
		return errors.New("cannot combine -only and -except flags")
	}
	filtered := make([]*token.Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		if len(onlyFlag) == 0 && len(exceptFlag) == 0 ||
			len(onlyFlag) != 0 && includes(onlyFlag, iface.Name) ||
			len(exceptFlag) != 0 && !includes(exceptFlag, iface.Name) {
			filtered = append(filtered, iface)
		}
	}
	return printer.Print(os.Stdout, filtered,
		printer.WithPackageName(packageFlag),
		printer.WithGofmt(gofmtFlag),
		printer.WithPrefixes(prefixesFlag),
	)
}

func connect(system bool) (*dbus.Conn, error) {
	if system {
		return dbus.SystemBus()
	}
	return dbus.SessionBus()
}

func parseDest(conn *dbus.Conn, dests []string) ([]*token.Interface, error) {
	ifaces := make([]*token.Interface, 0, 16)
	for _, dest := range dests {
		if err := introspectDest(conn, dest, "/", func(node *introspect.Node) error {
			chunk, err := parser.ParseNode(node)
			if err != nil {
				return err
			}
			ifaces = merge(ifaces, chunk)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return ifaces, nil
}

func generateXML(conn *dbus.Conn, dests []string) ([]byte, error) {
	var ifaces []introspect.Interface
	for _, dest := range dests {
		if err := introspectDest(conn, dest, "/", func(n *introspect.Node) error {
			for _, ifn := range n.Interfaces {
				var found bool
				for _, ifc := range ifaces {
					if ifc.Name == ifn.Name {
						found = true
						break
					}
				}
				if !found {
					ifaces = append(ifaces, ifn)
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return xml.MarshalIndent(&introspect.Node{
		Interfaces: ifaces,
	}, "", "\t")
}

func merge(curr, next []*token.Interface) []*token.Interface {
	for _, ifn := range next {
		var found bool
		for _, ifc := range curr {
			if ifc.Name == ifn.Name {
				found = true
				break
			}
		}
		if !found {
			curr = append(curr, ifn)
		}
	}
	return curr
}

func includes(ss []string, s string) bool {
	for i := range ss {
		if ss[i] == s {
			return true
		}
	}
	return false
}

func introspectDest(
	conn *dbus.Conn, dest string, path dbus.ObjectPath,
	fn func(node *introspect.Node) error,
) error {
	var s string
	if err := conn.Object(dest, path).Call(
		"org.freedesktop.DBus.Introspectable.Introspect", 0,
	).Store(&s); err != nil {
		return err
	}
	var node introspect.Node
	if err := xml.Unmarshal([]byte(s), &node); err != nil {
		return err
	}
	if err := fn(&node); err != nil {
		return err
	}
	if path == "/" {
		path = ""
	}
	for _, child := range node.Children {
		if err := introspectDest(conn, dest, path+"/"+dbus.ObjectPath(child.Name), fn); err != nil {
			return err
		}
	}
	return nil
}
