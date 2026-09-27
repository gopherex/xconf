package env_test

import (
	"context"
	"testing"
	"time"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/env"
)

func schema(fields ...sp.FieldDef) *sp.Schema {
	return sp.NewSchema(sp.ID("test", "env", sp.Ver(1, 0, 0))).Coerce().Fields(fields...).MustBuild()
}
func TestEnvironmentSchemaMapping(t *testing.T) {
	s := schema(sp.Object("httpServer", sp.Str("host"), sp.Int64("port")), sp.List("timeouts", sp.Duration("")), sp.MapOf("limits", sp.Int64("value")), sp.Str("empty").Default("default"))
	source := env.New(env.Prefix("APP_"), env.Environment([]string{"APP_HTTP_SERVER_HOST=example", "APP_HTTP_SERVER_PORT=8080", "APP_TIMEOUTS=1s,2s", "APP_LIMITS={\"cpu\":9007199254740993}", "APP_EMPTY="}), env.Split("APP_TIMEOUTS", ","))
	got, err := x.LoadAs[map[string]any](context.Background(), s, source)
	if err != nil {
		t.Fatal(err)
	}
	if got["empty"] != "" || got["httpServer"].(map[string]any)["port"] != int64(8080) || got["timeouts"].([]any)[1] != 2*time.Second || got["limits"].(map[string]any)["cpu"] != int64(9007199254740993) {
		t.Fatal(got)
	}
}
func TestExplicitBindingsAndEmptySplit(t *testing.T) {
	s := schema(sp.MapOf("labels", sp.Str("value")), sp.List("list", sp.Str("")))
	got, err := x.LoadAs[map[string]any](context.Background(), s, env.New(env.Environment([]string{"CUSTOM=yes", "LIST="}), env.Bind("CUSTOM", "labels", "a.b"), env.Split("LIST", ",")))
	if err != nil || got["labels"].(map[string]any)["a.b"] != "yes" || len(got["list"].([]any)) != 0 {
		t.Fatal(got, err)
	}
}
func TestAmbiguousAndOverlappingInput(t *testing.T) {
	cases := []struct {
		s    *sp.Schema
		opts []env.Option
	}{
		{schema(sp.Str("httpServer"), sp.Str("http_server")), nil},
		{schema(sp.Object("db", sp.Int64("port"))), []env.Option{env.Environment([]string{"DB={\"port\":1}", "DB_PORT=2"})}},
		{schema(sp.Int64("n")), []env.Option{env.Prefix("APP_"), env.Strict(), env.Environment([]string{"APP_TYPO=3"})}},
		{schema(sp.List("ports", sp.Int64(""))), []env.Option{env.Environment([]string{"PORTS=[1] true"})}},
	}
	for i, c := range cases {
		_, err := x.Load(context.Background(), c.s, env.New(c.opts...))
		if err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
func TestAbsentInjectedEnvironmentDoesNotReadOS(t *testing.T) {
	t.Setenv("N", "99")
	v, err := x.LoadAs[map[string]any](context.Background(), schema(sp.Int64("n").Default(7)), env.New(env.Environment(nil)))
	if err != nil || v["n"] != int64(7) {
		t.Fatal(v, err)
	}
}

func TestOneOfEncodingCannotDependOnMapIteration(t *testing.T) {
	s := schema(sp.OneOf("job", "kind").Variant("text", sp.Str("kind"), sp.Str("value")).Variant("object", sp.Str("kind"), sp.Object("value", sp.Int64("n"))))
	for i := 0; i < 10; i++ {
		_, err := x.Load(context.Background(), s, env.New(env.Environment([]string{"JOB_KIND=text", "JOB_VALUE=hello"})))
		if err == nil {
			t.Fatal("ambiguous encoding accepted")
		}
	}
	_, err := x.Load(context.Background(), s, env.New(env.Environment([]string{`JOB={"kind":"object","value":{"n":"7"}}`})))
	if err != nil {
		t.Fatal(err)
	}
}
