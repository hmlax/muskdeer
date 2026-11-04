package deer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

func decodeYAML(ctx context.Context, data []byte, target proto.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > maximumConfigBytes {
		return configFailure("YAML size", errors.New("invalid document size"))
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return configFailure("YAML decode", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return configFailure("YAML documents", errors.New("exactly one document is required"))
	}
	var buffer bytes.Buffer
	if err := yamlJSON(&buffer, &document, 0); err != nil {
		return configFailure("YAML values", err)
	}
	if buffer.Len() > maximumConfigBytes {
		return configFailure("YAML size", errors.New("encoded document exceeds limit"))
	}
	decoderOptions := protojson.UnmarshalOptions{
		DiscardUnknown: false,
		RecursionLimit: 64,
	}
	if err := decoderOptions.Unmarshal(buffer.Bytes(), target); err != nil {
		return configFailure("YAML schema", err)
	}
	return ctx.Err()
}

// yamlJSON translates the actual input syntax without floating-point values or
// generic business maps. Protobuf then owns field/type/duplicate validation.
func yamlJSON(out *bytes.Buffer, n *yaml.Node, depth int) error {
	if depth > 64 || n.Anchor != "" || n.Kind == yaml.AliasNode {
		return errors.New("YAML depth, anchors or aliases are unsupported")
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) != 1 {
			return errors.New("empty YAML document")
		}
		return yamlJSON(out, n.Content[0], depth+1)
	case yaml.MappingNode:
		if n.Tag != "!!map" {
			return errors.New("unsupported mapping tag")
		}
		out.WriteByte('{')
		seen := make(map[string]bool)
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Anchor != "" || seen[key.Value] || key.Value == "<<" {
				return errors.New("YAML mapping keys must be unique strings")
			}
			seen[key.Value] = true
			if i > 0 {
				out.WriteByte(',')
			}
			encoded, _ := json.Marshal(key.Value)
			out.Write(encoded)
			out.WriteByte(':')
			if err := yamlJSON(out, n.Content[i+1], depth+1); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case yaml.SequenceNode:
		if n.Tag != "!!seq" {
			return errors.New("unsupported sequence tag")
		}
		out.WriteByte('[')
		for i, item := range n.Content {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := yamlJSON(out, item, depth+1); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case yaml.ScalarNode:
		return yamlScalarJSON(out, n)
	default:
		return errors.New("unsupported YAML node")
	}
	return nil
}

func encodeYAML(message proto.Message) ([]byte, error) {
	data, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(message)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	var block func(*yaml.Node)
	block = func(n *yaml.Node) {
		n.Style = 0
		for _, child := range n.Content {
			block(child)
		}
	}
	block(&node)
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func yamlScalarJSON(out *bytes.Buffer, n *yaml.Node) error {
	switch n.Tag {
	case "!!str":
		v, _ := json.Marshal(n.Value)
		out.Write(v)
	case "!!bool":
		if n.Value != "true" && n.Value != "false" {
			return errors.New("noncanonical boolean")
		}
		out.WriteString(n.Value)
	case "!!null":
		out.WriteString("null")
	case "!!int":
		if n.Value == "" || (n.Value[0] < '0' || n.Value[0] > '9') && n.Value[0] != '-' || !json.Valid([]byte(n.Value)) || strings.ContainsAny(n.Value, ".eE") {
			return errors.New("integer must use decimal JSON syntax")
		}
		out.WriteString(n.Value)
	default:
		return errors.New("unsupported scalar; decimal values must be quoted strings")
	}
	return nil
}
