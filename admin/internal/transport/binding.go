package transport

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tx7do/go-utils/ossutil"
	"github.com/tx7do/go-utils/trans"

	apiV1 "github.com/hmlax/muskdeer/admin/biz/model/admin/v1"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var requestJSON = protojson.UnmarshalOptions{DiscardUnknown: true}

// BindRequest decodes the actual HTTP boundary directly into its protobuf type.
func BindRequest(c *app.RequestContext, message proto.Message, body string) error {
	if upload, ok := message.(*apiV1.UploadFileRequest); ok && strings.HasPrefix(string(c.Request.Header.ContentType()), "multipart/form-data") {
		if err := bindUpload(c, upload); err != nil {
			return err
		}
	} else if body != "" && len(c.Request.Body()) > 0 {
		target := message.ProtoReflect()
		if body != "*" {
			for _, name := range strings.Split(body, ".") {
				field := findField(target, name)
				if field == nil || field.Message() == nil {
					return operatorV1.ErrorAdminBadRequest(errorInvalidBodyBinding)
				}
				target = target.Mutable(field).Message()
			}
		}
		if err := requestJSON.Unmarshal(c.Request.Body(), target.Interface()); err != nil {
			return operatorV1.ErrorAdminBadRequest("invalid JSON request: %v", err)
		}
	}
	values := make(map[string][]string)
	c.QueryArgs().VisitAll(func(key, value []byte) { values[string(key)] = append(values[string(key)], string(value)) })
	for key, items := range values {
		if err := bindField(message.ProtoReflect(), strings.Split(key, "."), items); err != nil {
			return operatorV1.ErrorAdminBadRequest("invalid query %s: %v", key, err)
		}
	}
	raw, _ := c.Get(pathValuesKey)
	params, _ := raw.(map[string]string)
	for key, value := range params {
		if err := bindField(message.ProtoReflect(), strings.Split(key, "."), []string{value}); err != nil {
			return operatorV1.ErrorAdminBadRequest("invalid path %s: %v", key, err)
		}
	}
	return nil
}
func findField(message protoreflect.Message, name string) protoreflect.FieldDescriptor {
	fields := message.Descriptor().Fields()
	if field := fields.ByName(protoreflect.Name(name)); field != nil {
		return field
	}
	return fields.ByJSONName(name)
}
func bindField(message protoreflect.Message, path []string, values []string) error {
	name := strings.TrimSuffix(path[0], "[]")
	field := findField(message, name)
	if field == nil {
		return nil
	} // HTTP-only query options are outside the protobuf DTO.
	if len(path) > 1 {
		if field.Message() == nil || field.IsList() || field.IsMap() {
			return fmt.Errorf("%s is not a message", name)
		}
		return bindField(message.Mutable(field).Message(), path[1:], values)
	}
	if len(values) == 0 {
		return nil
	}
	if oneof := field.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
		if existing := message.WhichOneof(oneof); existing != nil && existing.Number() != field.Number() {
			return fmt.Errorf("multiple values for oneof %s", oneof.Name())
		}
	}
	if field.IsList() {
		list := message.Mutable(field).List()
		for _, raw := range values {
			value, err := parseValue(field, raw, list.NewElement)
			if err != nil {
				return err
			}
			list.Append(value)
		}
		return nil
	}
	if field.IsMap() {
		return fmt.Errorf("map query fields require a JSON body")
	}
	if len(values) != 1 {
		return fmt.Errorf("multiple values for %s", name)
	}
	if values[0] == "" {
		return nil
	}
	value, err := parseValue(field, values[0], func() protoreflect.Value { return message.NewField(field) })
	if err != nil {
		return err
	}
	message.Set(field, value)
	return nil
}
func parseValue(field protoreflect.FieldDescriptor, raw string, newValue func() protoreflect.Value) (protoreflect.Value, error) {
	switch field.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(raw), nil
	case protoreflect.BoolKind:
		value, err := strconv.ParseBool(raw)
		return protoreflect.ValueOfBool(value), err
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		value, err := strconv.ParseInt(raw, 10, 32)
		return protoreflect.ValueOfInt32(int32(value)), err
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		value, err := strconv.ParseInt(raw, 10, 64)
		return protoreflect.ValueOfInt64(value), err
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		value, err := strconv.ParseUint(raw, 10, 32)
		return protoreflect.ValueOfUint32(uint32(value)), err
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		value, err := strconv.ParseUint(raw, 10, 64)
		return protoreflect.ValueOfUint64(value), err
	case protoreflect.EnumKind:
		if value := field.Enum().Values().ByName(protoreflect.Name(raw)); value != nil {
			return protoreflect.ValueOfEnum(value.Number()), nil
		}
		value, err := strconv.ParseInt(raw, 10, 32)
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(value)), err
	case protoreflect.BytesKind:
		value, err := base64.StdEncoding.DecodeString(raw)
		return protoreflect.ValueOfBytes(value), err
	case protoreflect.MessageKind:
		value := newValue()
		message := value.Message()
		switch message.Descriptor().FullName() {
		case "google.protobuf.FieldMask", "google.protobuf.Timestamp", "google.protobuf.Duration":
			raw = strconv.Quote(raw)
		}
		if err := requestJSON.Unmarshal([]byte(raw), message.Interface()); err != nil {
			return protoreflect.Value{}, err
		}
		return value, nil
	default:
		return protoreflect.Value{}, fmt.Errorf("unsupported query field %s", field.FullName())
	}
}
func ValidateRequest(value proto.Message) error {
	if validator, ok := value.(interface{ ValidateAll() error }); ok {
		if err := validator.ValidateAll(); err != nil {
			return operatorV1.ErrorAdminBadRequest("invalid request: %v", err)
		}
	} else if validator, ok := value.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return operatorV1.ErrorAdminBadRequest("invalid request: %v", err)
		}
	}
	return nil
}
func bindUpload(c *app.RequestContext, upload *apiV1.UploadFileRequest) error {
	form, err := c.MultipartForm()
	if err != nil {
		return operatorV1.ErrorAdminBadRequest("invalid multipart upload: %v", err)
	}
	defer func() {
		if closeErr := form.RemoveAll(); closeErr != nil {
			slog.Error("remove multipart temporary files", "error", closeErr)
		}
	}()
	files := form.File["file"]
	if len(files) == 0 {
		return operatorV1.ErrorAdminBadRequest("file is required")
	}
	file, err := files[0].Open()
	if err != nil {
		return operatorV1.ErrorAdminBadRequest("open upload: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.Error("close multipart upload", "error", closeErr)
		}
	}()
	data, err := io.ReadAll(io.LimitReader(file, ossutil.MaxUploadSize+1))
	if err != nil {
		return operatorV1.ErrorAdminBadRequest("read upload: %v", err)
	}
	if int64(len(data)) > ossutil.MaxUploadSize {
		return operatorV1.ErrorStorageFileTooLarge("file exceeds maximum upload size")
	}
	upload.Source = &apiV1.UploadFileRequest_File{File: data}
	upload.SourceFileName = trans.Ptr(files[0].Filename)
	upload.Mime = trans.Ptr(files[0].Header.Get("Content-Type"))
	upload.Size = trans.Ptr(int64(len(data)))
	if name := c.PostForm("sourceFileName"); name != "" {
		upload.SourceFileName = &name
	}
	if contentType := c.PostForm("mime"); contentType != "" {
		upload.Mime = &contentType
	}
	if raw := c.PostForm("storageObject"); raw != "" {
		upload.StorageObject = new(apiV1.StorageObject)
		if err := requestJSON.Unmarshal([]byte(raw), upload.StorageObject); err != nil {
			return operatorV1.ErrorAdminBadRequest("invalid storageObject: %v", err)
		}
	}
	return nil
}
