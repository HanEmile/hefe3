// Example KScope plugin: struct-diff
// Build with: go build -buildmode=plugin -o struct-diff.so struct_diff.go
//
// This plugin compares two struct definitions (kernel vs uapi) and returns
// a field-level diff. Drop the compiled .so into the plugin directory.

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hanemile/kscope/internal/plugin"
)

// Plugin is the required export symbol — must be of type func() plugin.Plugin.
func Plugin() plugin.Plugin { return &structDiffPlugin{} }

type structDiffPlugin struct{}

func (p *structDiffPlugin) Meta() plugin.Metadata {
	return plugin.Metadata{
		Name:        "struct-diff",
		Version:     "1.0.0",
		Description: "Field-level diff between kernel and uapi definitions of the same struct name",
		Actions: []plugin.ActionDescriptor{
			{
				ID: "1",
				Label: "struct-diff-action",
				Description: "Field-level diff between kernel and uapi definitions of the same struct name",
				Params:      []plugin.ParamDef{
					{
						Name: "name",
						Kind: plugin.ParamString,
						Label: "name",
						Required: true,
						Default: "",
					},
					{
						Name: "kernel_file",
						Kind: plugin.ParamString,
						Label: "kernel_file",
						Required: true,
						Default: "",
					},
					{
						Name: "uapi_file",
						Kind: plugin.ParamString,
						Label: "uapi_file",
						Required: true,
						Default: "",
					},
				},
			},
		},
		Author:      "example",
	}
}

type diffParams struct {
	Name       string `json:"name"`
	KernelFile string `json:"kernel_file"`
	UAPIFile   string `json:"uapi_file"`
}

func (p *structDiffPlugin) Execute(q plugin.Query) plugin.Result {
	switch q.Action {
	case "diff":
		var params diffParams
		if err := json.Unmarshal(q.Params, &params); err != nil {
			return plugin.Result{Error: "bad params: " + err.Error()}
		}
		return p.diffStructs(q.KernelRoot, params)
	default:
		return plugin.Result{Error: "unknown action: " + q.Action}
	}
}

var reField = regexp.MustCompile(`^\s+(\w[\w\s\*\[\]]*)\s+(\w+)\s*(?::\s*\d+)?\s*;`)

func parseFields(path string, structName string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var fields []string
	inStruct := false
	depth := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !inStruct {
			if strings.Contains(line, "struct "+structName) {
				inStruct = true
			}
			continue
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth <= 0 {
			break
		}
		if m := reField.FindStringSubmatch(line); m != nil {
			fields = append(fields, strings.TrimSpace(m[1])+" "+m[2])
		}
	}
	return fields, scanner.Err()
}

func (p *structDiffPlugin) diffStructs(root string, params diffParams) plugin.Result {
	kPath := filepath.Join(root, params.KernelFile)
	uPath := filepath.Join(root, params.UAPIFile)

	kFields, err1 := parseFields(kPath, params.Name)
	uFields, err2 := parseFields(uPath, params.Name)

	errs := ""
	if err1 != nil {
		errs += fmt.Sprintf("kernel: %v; ", err1)
	}
	if err2 != nil {
		errs += fmt.Sprintf("uapi: %v; ", err2)
	}

	kSet := make(map[string]bool)
	uSet := make(map[string]bool)
	for _, f := range kFields {
		kSet[f] = true
	}
	for _, f := range uFields {
		uSet[f] = true
	}

	var onlyKernel, onlyUAPI, common []string
	for _, f := range kFields {
		if uSet[f] {
			common = append(common, f)
		} else {
			onlyKernel = append(onlyKernel, f)
		}
	}
	for _, f := range uFields {
		if !kSet[f] {
			onlyUAPI = append(onlyUAPI, f)
		}
	}

	return plugin.Result{
		Data: map[string]interface{}{
			"struct":       params.Name,
			"common":       common,
			"only_kernel":  onlyKernel,
			"only_uapi":    onlyUAPI,
			"kernel_count": len(kFields),
			"uapi_count":   len(uFields),
			"errors":       errs,
		},
	}
}
