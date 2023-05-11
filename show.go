package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/template"

	"github.com/nokia/ntt/internal/env"
	"github.com/nokia/ntt/internal/fs"
	"github.com/nokia/ntt/internal/yaml"
	"github.com/nokia/ntt/project"
	"github.com/nokia/ntt/ttcn3"
	"github.com/nokia/ntt/ttcn3/syntax"
	"github.com/spf13/cobra"
)

var (
	ShowCommand = &cobra.Command{
		Use:   "show [ <file>...] [-- var...]",
		Short: "Show test suite configuration.",
		RunE:  show,
	}
)

func show(cmd *cobra.Command, args []string) error {
	_, keys := splitArgs(args, cmd.ArgsLenAtDash())

	r := ConfigReport{Config: Project}
	r.Environ = Project.Variables.Slice()
	r.Files, r.err = project.Files(Project)

	switch {
	case outputJSON:
		return printJSON(&r, keys)
	case ShSetup:
		return printShellScript(&r, keys)
	case len(keys) != 0:
		return printValues(Project, keys)
	default:
		keys := []string{
			"name",
			"root",
			"source_dir",
			"sources",
			"imports",
			"parameters_file",
			"hooks_file",
			"lint_file",
		}
		return printKeyValues(Project, keys)
	}
}

func printJSON(report *ConfigReport, keys []string) error {
	if len(keys) != 0 {
		return fmt.Errorf("command line option --json does not accept additional command line arguments")
	}

	var presets []string
	if s := env.Getenv("NTT_PRESETS"); s != "" {
		presets = strings.Split(s, string(os.PathListSeparator))
	}

	params, err := ApplyPresets(report.Config, presets...)
	if err != nil {
		return err
	}
	report.Config.Parameters = *params

	b, err := yaml.MarshalJSON(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}
	fmt.Println(string(b))
	return report.err
}

// ApplyPresets returns a list of test case configurations with optional
// presets applied. The presets are applied in the order they are specified in
// the list.
//
// ApplyPresets reads test case configuration from environment variables, the
// parameters file, package.yml and from the TTCN-3 documentation tags.
func ApplyPresets(c *project.Config, presets ...string) (*project.Parameters, error) {
	// Global configuration
	gc := c.Parameters

	// Presets override/extend parameters files
	for _, preset := range presets {
		tc, ok := gc.Presets[preset]
		if !ok {
			return nil, fmt.Errorf("preset %q not found", preset)
		}
		gc.TestConfig = project.MergeTestConfig(gc.TestConfig, tc)
	}

	files, err := fs.TTCN3Files(c.Sources...)
	if err != nil {
		return nil, err
	}

	var list []project.TestConfig
	for _, file := range files {
		tree := ttcn3.ParseFile(file)
		tree.Inspect(func(n syntax.Node) bool {
			switch n := n.(type) {
			case *syntax.FuncDecl:
				if n.IsTest() || n.IsControl() {
					list = append(list, gc.Glob(tree.QualifiedName(n), presets...)...)
				}
				return false
			case *syntax.ControlPart:
				list = append(list, gc.Glob(tree.QualifiedName(n), presets...)...)
				return false
			}
			return true
		})
	}
	gc.Execute = list
	return &gc, nil
}

func printShellScript(report *ConfigReport, keys []string) error {
	const shellTemplate = `# This is a generated output of ntt show. Args: {{ .Args }}

# k3-hook calls the K3 test hook (if defined) with action passed by $1.
function k3-hook()
{
    if [ -n "$K3_HOOKS_FILE" ]; then
        K3_SOURCES="${K3_SOURCES[*]}" \
        K3_IMPORTS="${K3_IMPORTS[*]}" \
        K3_TTCN3_FILES="${K3_TTCN3_FILES[*]}" \
            "$K3_HOOKS_FILE" "$@" 1>&2
    fi
}

{{ if .Name           -}} export K3_NAME='{{ .Name }}'                      {{- end }}
{{ if .HooksFile      -}} export K3_HOOKS_FILE='{{ .HooksFile }}'           {{- end }}
{{ if .Root           -}} export K3_SOURCE_DIR='{{ .Root }}'                {{- end }}

{{ range .Environ }}export '{{.}}'
{{end}}

K3_SOURCES=(
{{ range .Sources }}	{{.}}
{{end}})

K3_IMPORTS=(
{{ range .Imports }}	{{.}}
{{end}})

K3_TTCN3_FILES=(
{{ range .Files }}	{{.}}
{{end}})

K3_BUILTINS=(
{{ range .K3.Includes }}	{{.}}
{{end}})

{{ if .Err }}
# ERROR
#
# Output might not be complete, because some errors have occurred during
# execution. We return "false", to give you the chance to detect this
# situation
read -r -d '' K3_ERROR <<'EOF'
{{.Err}}
EOF
false
{{ end }}
`
	if len(keys) != 0 {
		return fmt.Errorf("command line option --sh does not accept additional command line arguments")
	}

	t := template.Must(template.New("k3-sh-setup").Parse(shellTemplate))
	if err := t.Execute(os.Stdout, report); err != nil {
		fmt.Printf(`
# ERROR: Internal template did not compile: %s
#
# Output might not be complete, because some errors have occurred during
# execution. We return "false", to give you the chance to detect this
# situation
false
`, err.Error())
		return err
	}

	if err := report.Err(); err != "" {
		return fmt.Errorf("%s", err)
	}
	return nil
}

func get(c *project.Config, key string) ([]string, error) {
	b, err := yaml.MarshalJSON(c)
	if err != nil {
		return nil, err
	}

	conf := make(map[string]interface{})
	if err := json.Unmarshal(b, &conf); err != nil {
		return nil, err
	}

	for _, k := range strings.Split(key, ".") {
		v, ok := conf[k]
		if !ok {
			return nil, fmt.Errorf("key %q not found", k)
		}

		switch v := v.(type) {
		case []string:
			return v, nil
		case map[string]string:
			s := make([]string, 0, len(v))
			for key, val := range v {
				s = append(s, fmt.Sprintf("'%s=%s'", key, val))
			}
			sort.Strings(s)
			return s, nil
		case map[string]interface{}:
			conf = v
		default:
			return []string{fmt.Sprintf("%v", v)}, nil
		}
	}
	return nil, fmt.Errorf("value of key %q is not of type string or list of strings", key)
}

func printValues(c *project.Config, keys []string) error {
	for _, key := range keys {
		s, err := get(c, key)
		if err != nil {
			return err
		}
		for _, v := range s {
			fmt.Println(v)
		}
	}
	return nil
}

func printKeyValues(c *project.Config, keys []string) error {
	for _, key := range keys {
		s, err := get(c, key)
		if err != nil {
			return err
		}
		if len(s) > 0 {
			fmt.Printf("NTT_%s=\"%s\"\n", strings.ToUpper(key), strings.Join(s, " "))
		}
	}

	return nil
}

// splitArgs splits an argument list at pos. Pos is usually the position of '--'
// (see cobra.Command.ArgsLenAtDash).
//
// Is pos < 0, the second list will be empty
func splitArgs(args []string, pos int) ([]string, []string) {
	if pos < 0 {
		return args, []string{}
	}
	return args[:pos], args[pos:]
}

type ConfigReport struct {
	Args            []string `json:"args"`
	*project.Config `json:",inline"`
	Environ         []string `json:"env"`
	Files           []string `json:"files"`
	err             error
}

func (r *ConfigReport) Err() string {
	if r.err != nil {
		return r.err.Error()
	}
	return ""
}
