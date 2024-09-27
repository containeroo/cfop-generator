package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
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

var (
	zonefilePath string
	proxied      bool
)

const (
	zonefileParseRegex string = `(.*)\s(\d+)\sIN\s([A-Z]+)\s(.*)`
	dnsrecordTemplate  string = `
{{- range . }}
---
apiVersion: cloudflare-operator.io/v1
kind: DNSRecord
metadata:
  name: {{ .Name | cleanName }}
spec:
  name: {{ .Name }}
  proxied: {{ .Proxied }}
  ttl: {{ .TTL }}
  type: {{ .Type }}
{{- if and (ne .Type "SRV") (ne .Type "MX") }}
  content: {{ .Content | trimDot }}
{{- end }}
{{- if (eq .Type "SRV") }}
{{- $d := split .Content " " }}
  data:
    priority: {{ index $d 0 }}
    weight: {{ index $d 1 }}
    port: {{ index $d 2 }}
    target: {{ index $d 3 | trimDot }}
{{- end }}
{{- if (eq .Type "MX") }}
{{ $d := split .Content " " }}
  priority: {{ index $d 0 }}
  content: {{ index $d 1 | trimDot }}
{{- end }}
{{- end }}`
)

func parseFlags() {
	flag.StringVar(&zonefilePath, "file", "", "Path to the exported zonefile")
	flag.BoolVar(&proxied, "proxied", true, "Whether the records should be proxied")
	flag.Parse()

	if zonefilePath == "" {
		fmt.Fprintf(os.Stderr, "flag -file is required\n")
		os.Exit(1)
	}
}

func parseZonefile(zonefile []byte) []DNSRecord {
	regex := regexp.MustCompile(zonefileParseRegex)

	var records []DNSRecord

	for _, line := range strings.Split(string(zonefile), "\n") {
		if match := regex.MatchString(line); match {
			name := strings.TrimSuffix(regex.FindStringSubmatch(line)[1], ".")
			ttl := regex.FindStringSubmatch(line)[2]
			recordType := regex.FindStringSubmatch(line)[3]
			content := regex.FindStringSubmatch(line)[4]

			if recordType == "SOA" || recordType == "NS" {
				continue
			}

			record := DNSRecord{
				Name:    name,
				Type:    recordType,
				TTL:     ttl,
				Content: content,
				Proxied: proxied,
			}

			records = append(records, record)
		}
	}

	return records
}

func renderTemplate(out io.Writer, records []DNSRecord) error {
	funcMap := template.FuncMap{
		"split": strings.Split,
		"trimDot": func(s string) string {
			return strings.TrimSuffix(s, ".")
		},
		"cleanName": func(s string) string {
			noDots := strings.ReplaceAll(s, ".", "-")
			noUnderscores := strings.ReplaceAll(noDots, "_", "-")
			noAsterisks := strings.ReplaceAll(noUnderscores, "*", "-")
			return noAsterisks
		},
	}

	tmpl, err := template.New("dnsrecord.yaml.tmpl").Funcs(funcMap).Parse(dnsrecordTemplate)
	if err != nil {
		return err
	}

	err = tmpl.Execute(out, records)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	parseFlags()

	zonefile, err := os.ReadFile(zonefilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	records := parseZonefile(zonefile)
	if err := renderTemplate(os.Stdout, records); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
