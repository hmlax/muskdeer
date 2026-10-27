// Command contractcheck validates owned Go syntax and compiled Proto schemas.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/hmlax/muskdeer/internal/policy"
)

func main() {
	flags := flag.NewFlagSet("contractcheck", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	path := flags.String("descriptor", "", "compiled project FileDescriptorSet")
	if err := flags.Parse(os.Args[1:]); err != nil || *path == "" || flags.NArg() != 0 {
		os.Exit(2)
	}
	issues, err := check(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, issue := range issues {
		fmt.Fprintln(os.Stderr, policy.Format(issue))
	}
	if len(issues) != 0 {
		os.Exit(1)
	}
	if _, err := fmt.Fprintln(os.Stdout, "Owned Go/Proto decimal and generated-model policies passed"); err != nil {
		os.Exit(1)
	}
}

func check(path string) ([]policy.Issue, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	var set descriptorpb.FileDescriptorSet
	if len(data) > 16*1024*1024 || proto.Unmarshal(data, &set) != nil || len(set.File) == 0 {
		return nil, errors.New("project descriptor set is missing or invalid")
	}
	issues := policy.Descriptors(&set)
	for _, root := range []string{"api", "cmd", "deer", "internal", "scripts", "third_party"} {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			issues = append(issues, policy.GoSource(fileSet, file)...)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return issues, nil
}
