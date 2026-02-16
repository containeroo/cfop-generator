package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

type DNSRecord struct {
	Name    string
	Type    string
	TTL     string
	Content string
	Proxied bool
}

type convertConfig struct {
	zonefilePath string
	proxied      bool
	outputPath   string
}

const (
	cliName           = "cfop-generator"
	zonefileParseExp  = `^(\S+)\s+(\d+)\s+IN\s+([A-Z]+)\s+(.+)$`
	version           = "v0.1.0"
	dnsrecordTemplate = `
{{- range . }}
---
apiVersion: cloudflare-operator.io/v1
kind: DNSRecord
metadata:
  name: {{ .Name | cleanName }}
spec:
  name: {{ .Name | yamlQuote }}
  proxied: {{ .Proxied }}
  ttl: {{ .TTL }}
  type: {{ .Type }}
{{- if and (ne .Type "SRV") (ne .Type "MX") }}
  content: {{ .Content | trimDot | yamlQuote }}
{{- end }}
{{- if (eq .Type "SRV") }}
{{- $d := split .Content }}
  data:
    priority: {{ index $d 0 }}
    weight: {{ index $d 1 }}
    port: {{ index $d 2 }}
    target: {{ index $d 3 | trimDot | yamlQuote }}
{{- end }}
{{- if (eq .Type "MX") }}
{{- $d := split .Content }}
  priority: {{ index $d 0 }}
  content: {{ index $d 1 | trimDot | yamlQuote }}
{{- end }}
{{- end }}`
)

var zonefileRegex = regexp.MustCompile(zonefileParseExp)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, filepath.Base(os.Args[0])))
}

func run(args []string, stdout io.Writer, stderr io.Writer, binaryName string) int {
	if binaryName == "" {
		binaryName = cliName
	}

	if len(args) > 0 {
		switch args[0] {
		case "completion":
			return runCompletion(args[1:], stdout, stderr, binaryName)
		case "version", "--version", "-version", "-v":
			fmt.Fprintln(stdout, version)
			return 0
		case "help", "-h", "--help":
			printUsage(stdout, binaryName)
			return 0
		}
	}

	cfg, err := parseConvertFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		printUsage(stderr, binaryName)
		return 2
	}

	zonefile, err := os.ReadFile(cfg.zonefilePath)
	if err != nil {
		fmt.Fprintf(stderr, "failed to read zonefile: %v\n", err)
		return 1
	}

	records, err := parseZonefile(zonefile, cfg.proxied)
	if err != nil {
		fmt.Fprintf(stderr, "failed to parse zonefile: %v\n", err)
		return 1
	}

	var output bytes.Buffer
	if err := renderTemplate(&output, records); err != nil {
		fmt.Fprintf(stderr, "failed to render template: %v\n", err)
		return 1
	}

	if cfg.outputPath == "" {
		if _, err := io.Copy(stdout, &output); err != nil {
			fmt.Fprintf(stderr, "failed to write output: %v\n", err)
			return 1
		}
		return 0
	}

	if err := writeOutputFile(cfg.outputPath, output.Bytes()); err != nil {
		fmt.Fprintf(stderr, "failed to write output file: %v\n", err)
		return 1
	}

	return 0
}

func parseConvertFlags(args []string) (convertConfig, error) {
	cfg := convertConfig{}
	fs := flag.NewFlagSet(cliName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.zonefilePath, "file", "", "Path to the exported zonefile")
	fs.BoolVar(&cfg.proxied, "proxied", true, "Whether the records should be proxied")
	fs.StringVar(&cfg.outputPath, "output", "", "Optional output file path (written with mode 0600)")

	if err := fs.Parse(args); err != nil {
		return convertConfig{}, err
	}

	if fs.NArg() > 0 {
		return convertConfig{}, fmt.Errorf("unexpected argument(s): %s", strings.Join(fs.Args(), " "))
	}

	if strings.TrimSpace(cfg.zonefilePath) == "" {
		return convertConfig{}, errors.New("flag -file is required")
	}

	return cfg, nil
}

func parseZonefile(zonefile []byte, proxied bool) ([]DNSRecord, error) {
	records := make([]DNSRecord, 0)

	for idx, rawLine := range strings.Split(string(zonefile), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "$") {
			continue
		}

		matches := zonefileRegex.FindStringSubmatch(line)
		if matches == nil {
			return nil, fmt.Errorf("line %d: invalid record format", idx+1)
		}

		recordType := matches[3]
		if recordType == "SOA" || recordType == "NS" {
			continue
		}

		content := matches[4]
		if recordType == "MX" {
			if len(strings.Fields(content)) != 2 {
				return nil, fmt.Errorf("line %d: MX record requires "+
					"priority and target", idx+1)
			}
		}
		if recordType == "SRV" {
			if len(strings.Fields(content)) != 4 {
				return nil, fmt.Errorf("line %d: SRV record requires "+
					"priority weight port target", idx+1)
			}
		}

		records = append(records, DNSRecord{
			Name:    strings.TrimSuffix(matches[1], "."),
			Type:    recordType,
			TTL:     matches[2],
			Content: content,
			Proxied: proxied,
		})
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Name != records[j].Name {
			return records[i].Name < records[j].Name
		}
		if records[i].Type != records[j].Type {
			return records[i].Type < records[j].Type
		}
		if records[i].TTL != records[j].TTL {
			return records[i].TTL < records[j].TTL
		}
		return records[i].Content < records[j].Content
	})

	return records, nil
}

func renderTemplate(out io.Writer, records []DNSRecord) error {
	funcMap := template.FuncMap{
		"split": strings.Fields,
		"trimDot": func(s string) string {
			return strings.TrimSuffix(s, ".")
		},
		"yamlQuote": func(s string) string {
			return strconv.Quote(s)
		},
		"cleanName": func(s string) string {
			s = strings.ToLower(s)
			replacer := strings.NewReplacer(".", "-", "_", "-", "*", "-")
			s = replacer.Replace(s)
			var b strings.Builder
			for _, r := range s {
				switch {
				case r >= 'a' && r <= 'z':
					b.WriteRune(r)
				case r >= '0' && r <= '9':
					b.WriteRune(r)
				case r == '-':
					b.WriteRune(r)
				default:
					b.WriteRune('-')
				}
			}
			clean := strings.Trim(b.String(), "-")
			if clean == "" {
				return "record"
			}
			return clean
		},
	}

	tmpl, err := template.New("dnsrecord.yaml.tmpl").Funcs(funcMap).Option("missingkey=error").Parse(dnsrecordTemplate)
	if err != nil {
		return err
	}

	return tmpl.Execute(out, records)
}

func runCompletion(args []string, stdout io.Writer, stderr io.Writer, binaryName string) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: completion bash|zsh")
		return 2
	}

	switch args[0] {
	case "bash":
		fmt.Fprint(stdout, bashCompletion(binaryName))
		return 0
	case "zsh":
		fmt.Fprint(stdout, zshCompletion(binaryName))
		return 0
	default:
		fmt.Fprintf(stderr, "unsupported shell %q (expected bash or zsh)\n", args[0])
		return 2
	}
}

func bashCompletion(binaryName string) string {
	funcName := strings.ReplaceAll(binaryName, "-", "_")
	return fmt.Sprintf(`# bash completion for %s
_%s_completion() {
  local cur words cword
  _init_completion || return

  if [[ ${cword} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "completion version" -- "${cur}") )
    return
  fi

  if [[ ${words[1]} == "completion" && ${cword} -eq 2 ]]; then
    COMPREPLY=( $(compgen -W "bash zsh" -- "${cur}") )
    return
  fi

  COMPREPLY=( $(compgen -W "-file -proxied -output --version" -- "${cur}") )
}
complete -F _%s_completion %s
`, binaryName, funcName, funcName, binaryName)
}

func zshCompletion(binaryName string) string {
	return fmt.Sprintf(`#compdef %s

local -a commands
commands=(
  'completion:Generate shell completion scripts'
  'version:Print the CLI version'
)

if (( CURRENT == 2 )); then
  _describe 'command' commands
  return
fi

case "${words[2]}" in
  completion)
    _values 'shell' bash zsh
    ;;
  *)
    _arguments '-file[Path to the exported zonefile]:file:_files' '--output[Output file written with mode 0600]:file:_files' '--proxied[Whether records should be proxied]' '--version[Print version]'
    ;;
esac
`, binaryName)
}

func writeOutputFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := file.Chmod(0o600); err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		return err
	}

	return nil
}

func printUsage(out io.Writer, binaryName string) {
	fmt.Fprintf(out, `Usage:
  %[1]s -file <zonefile> [-proxied=true|false] [-output <path>]
  %[1]s completion bash|zsh
  %[1]s version

Examples:
  %[1]s -file ./example.com.txt
  %[1]s -file ./example.com.txt -proxied=false -output ./records.yaml
  %[1]s completion zsh
`, binaryName)
}
