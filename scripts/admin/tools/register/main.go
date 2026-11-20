// register adds standard CRUD providers and the typed gateway manifest, then
// regenerates the transport adapters and Wire injector.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

type patch struct {
	path    string   // 目标文件
	anchors []string // 锚点行(按去空白后的全文匹配),插入到首个命中的锚点之后
	lines   []string // 插入的行(不含行尾符)
	skipIf  string   // 文件已包含该片段时跳过(幂等)
}

func main() {
	entity := flag.String("entity", "", "模块名:product / DictEntry / dict_entry 均可")
	appDir := flag.String("dir", "operator", "服务应用目录")
	flag.Parse()

	if *entity == "" {
		flag.Usage()
		os.Exit(2)
	}

	if err := registerCRUD(*entity, *appDir); err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		os.Exit(1)
	}
	script := filepath.Clean(filepath.Join(*appDir, "../scripts/admin/tools/gatewaygen.py"))
	command := exec.Command("python3", script)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "generate gateway adapters:", err)
		os.Exit(1)
	}
	wireCommand := exec.Command("go", "tool", "wire", "gen", "./"+filepath.Base(*appDir)+"/cmd/server")
	wireCommand.Dir = filepath.Clean(filepath.Join(*appDir, ".."))
	wireCommand.Stdout = os.Stdout
	wireCommand.Stderr = os.Stderr
	if err := wireCommand.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "generate operator Wire injector:", err)
		os.Exit(1)
	}
	fmt.Printf("Registered %s in Wire providers and admin/operator transport adapters.\n", *entity)
}

type gatewayManifest struct {
	Parameters      string            `json:"params"`
	Implementations map[string]string `json:"vars"`
	Services        map[string]string `json:"regs"`
}

func registerCRUD(entity, appDir string) error {
	typ := pascal(entity)
	name := lowerFirst(typ)
	if err := apply(
		// Register repository constructors without hard-coding their arguments.
		patch{
			path:   filepath.Join(appDir, "internal/data/providers.go"),
			skipIf: "New" + typ + "Repo,",
			anchors: []string{
				"// register:repo",
			},
			lines: []string{"\tNew" + typ + "Repo,"},
		},
		// Wire resolves constructor dependencies after transport generation.
		patch{
			path:   filepath.Join(appDir, "internal/service/providers.go"),
			skipIf: "New" + typ + "Service,",
			anchors: []string{
				"// register:service",
			},
			lines: []string{"\tNew" + typ + "Service,"},
		},
	); err != nil {
		return err
	}

	manifestPath := filepath.Clean(filepath.Join(appDir, "../scripts/admin/tools/gateway-services.json"))
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest gatewayManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	serviceName := typ + "Service"
	variable := name + "Service"
	if _, exists := manifest.Services[serviceName]; !exists {
		const anchor = "// register:param ── 新模块服务形参在此行后注册(make register 工具锚点,勿删)"
		if !strings.Contains(manifest.Parameters, anchor) {
			return fmt.Errorf("gateway manifest is missing the parameter anchor")
		}
		manifest.Parameters = strings.Replace(manifest.Parameters, anchor, anchor+"\n\t"+variable+" *service."+serviceName+",", 1)
		manifest.Services[serviceName] = variable
		manifest.Implementations[variable] = serviceName
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(manifestPath, append(encoded, '\n'), 0644); err != nil {
			return err
		}
	}
	return nil
}

// apply 依次应用各 patch;同文件多个 patch 顺序生效,幂等由 skipIf 保证。
func apply(patches ...patch) error {
	for _, p := range patches {
		if err := applyOne(p); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(p patch) error {
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return err
	}
	content := string(raw)
	if strings.Contains(content, p.skipIf) {
		return nil // 已登记,幂等跳过
	}

	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(content, eol)

	for _, anchor := range p.anchors {
		at := indexOfAnchor(lines, anchor)
		if at < 0 {
			continue
		}
		out := make([]string, 0, len(lines)+len(p.lines))
		out = append(out, lines[:at+1]...)
		out = append(out, p.lines...)
		out = append(out, lines[at+1:]...)
		return os.WriteFile(p.path, []byte(strings.Join(out, eol)), 0o644)
	}
	return fmt.Errorf("%s: 未找到注册锚点 %q,请检查锚点注释是否被移动或删除", p.path, p.anchors[0])
}

func indexOfAnchor(lines []string, anchor string) int {
	for i, line := range lines {
		if strings.TrimSpace(line) == anchor {
			return i
		}
	}
	return -1
}

// pascal 把 product / dict_entry / dictEntry 统一为 PascalCase 的实体名。
func pascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var b strings.Builder
	for _, p := range parts {
		r := []rune(p)
		b.WriteRune(unicode.ToUpper(r[0]))
		b.WriteString(string(r[1:]))
	}
	out := b.String()
	if out == "" {
		return s
	}
	return out
}

func lowerFirst(s string) string {
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}
