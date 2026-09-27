package pflag_test

import (
	"context"
	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	source "github.com/gopherex/xconf/contrib/sources/pflag"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"reflect"
	"testing"
	"time"
)

func TestChangedTypedValuesAndCapturedOwnership(t *testing.T) {
	f := flag.NewFlagSet("test", flag.ContinueOnError)
	f.String("untouched", "default", "")
	f.Bool("enabled", true, "")
	f.Int64("large", 0, "")
	f.StringSlice("names", nil, "")
	f.IntSlice("ports", nil, "")
	f.StringToString("labels", nil, "")
	f.Duration("delay", 0, "")
	if err := f.Parse([]string{"--enabled=false", "--large=9007199254740993", `--names="a,b",c`, "--ports=80,443", "--labels=a=b,c=d", "--delay=2s"}); err != nil {
		t.Fatal(err)
	}
	s, err := source.New(f, map[string]x.Path{"untouched": {"untouched"}, "enabled": {"enabled"}, "large": {"large"}, "names": {"names"}, "ports": {"ports"}, "labels": {"labels"}, "delay": {"delay"}})
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.Read(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := l.Values["untouched"]; exists {
		t.Fatal("default flag leaked")
	}
	if l.Values["enabled"] != false || l.Values["large"] != int64(9007199254740993) || l.Values["delay"] != 2*time.Second || !reflect.DeepEqual(l.Values["names"], []any{"a,b", "c"}) || !reflect.DeepEqual(l.Values["ports"], []any{int64(80), int64(443)}) {
		t.Fatal(l)
	}
	if err = f.Set("large", "4"); err != nil {
		t.Fatal(err)
	}
	l.Values["names"].([]any)[0] = "changed"
	again, err := s.Read(context.Background(), nil)
	if err != nil || again.Values["large"] != int64(9007199254740993) || again.Values["names"].([]any)[0] != "a,b" {
		t.Fatal(again, err)
	}
	schema := sp.NewSchema(sp.ID("test", "pflag", sp.Ver(1, 0, 0))).Fields(sp.Bool("enabled").Required()).MustBuild()
	only, err := source.New(f, map[string]x.Path{"enabled": {"enabled"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = x.Load(context.Background(), schema, only); err != nil {
		t.Fatal("typed bool required coercion", err)
	}
}
func TestCobraInheritedFlagsAliasesAndEmptySlices(t *testing.T) {
	root := &cobra.Command{Use: "app"}
	root.PersistentFlags().StringP("host", "H", "default", "")
	child := &cobra.Command{Use: "serve"}
	child.Flags().StringSlice("names", []string{"default"}, "")
	root.AddCommand(child)
	child.RunE = func(cmd *cobra.Command, _ []string) error {
		s, err := source.New(cmd.Flags(), map[string]x.Path{"host": {"db", "host"}, "names": {"names"}})
		if err != nil {
			return err
		}
		l, err := s.Read(context.Background(), nil)
		if err != nil {
			return err
		}
		if l.Values["db"].(map[string]any)["host"] != "localhost" || len(l.Values["names"].([]any)) != 0 {
			t.Fatal(l)
		}
		return nil
	}
	root.SetArgs([]string{"serve", "-H", "localhost", "--names="})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := source.New(child.Flags(), map[string]x.Path{"host": {"db"}, "names": {"db", "names"}}); err == nil {
		t.Fatal("overlap accepted")
	}
}
