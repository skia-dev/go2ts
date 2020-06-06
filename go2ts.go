// Package go2ts is a module for generating TypeScript definitions from Go
// structs.
package go2ts

import (
	"fmt"
	"go/ast"
	"html/template"
	"io"
	"reflect"
	"strings"
)

// Go2TS writes TypeScript interface definitions for the given Go structs.
type Go2TS struct {
	structs        []structRep
	nonStructs     []topLevelTSType
	seen           map[reflect.Type]string
	anonymousCount int
}

// New returns a new *StructToTS.
func New() *Go2TS {
	ret := &Go2TS{
		seen: map[reflect.Type]string{},
	}
	return ret
}

// Add a struct that needs a TypeScript definition.
//
// Just a wrapper for AddWithName with an interfaceName of "".
//
// See AddWithName.
func (g *Go2TS) Add(v interface{}) error {
	return g.AddWithName(v, "")
}

// AddWithName adds a struct that needs a TypeScript definition.
//
// The value passed in must resolve to a struct, a reflect.Type, or a
// reflect.Value of a struct. That is, a string or number for v will cause
// AddWithName to return an error, but a pointer to a struct is fine.
//
// The 'name' supplied will be the TypeScript interface name.  If
// 'interfaceName' is "" then the struct name will be used. If the struct is
// anonymous it will be given a name of the form "AnonymousN".
//
// The fields of the struct will be named following the convention for json
// serialization, including using the json tag if supplied.
//
// Fields tagged with `json:",omitempty"` will have "| null" added to their
// type.
//
// There is special handling of time.Time types to be TypeScript "string"s since
// they implement MarshalJSON, see
// https://pkg.go.dev/time?tab=doc#Time.MarshalJSON.
func (g *Go2TS) AddWithName(v interface{}, interfaceName string) error {
	var reflectType reflect.Type
	switch v := v.(type) {
	case reflect.Type:
		reflectType = v
	case reflect.Value:
		reflectType = v.Type()
	default:
		reflectType = reflect.TypeOf(v)
	}

	_, err := g.addType(reflectType, interfaceName)
	return err
}

// Render the TypeScript definitions to the given io.Writer.
func (g *Go2TS) Render(w io.Writer) error {
	_, err := fmt.Fprintln(w, "// DO NOT EDIT. This file is automatically generated.")
	if err != nil {
		return err
	}
	for _, st := range g.structs {
		if err := st.render(w); err != nil {
			return err
		}
	}

	for _, ns := range g.nonStructs {
		if _, err := fmt.Fprintln(w, ns.String()); err != nil {
			return err
		}
	}

	return nil
}

var primitive = map[reflect.Kind]bool{
	reflect.Bool:       true,
	reflect.Int:        true,
	reflect.Int8:       true,
	reflect.Int16:      true,
	reflect.Int32:      true,
	reflect.Int64:      true,
	reflect.Uint:       true,
	reflect.Uint8:      true,
	reflect.Uint16:     true,
	reflect.Uint32:     true,
	reflect.Uint64:     true,
	reflect.Uintptr:    true,
	reflect.Float32:    true,
	reflect.Float64:    true,
	reflect.Complex64:  true,
	reflect.Complex128: true,
	reflect.String:     true,
}

func isPrimitive(kind reflect.Kind) bool {
	return primitive[kind]
}

func (g *Go2TS) tsTypeFromReflectType(reflectType reflect.Type, calledFromAddType bool) *tsType {
	var ret tsType
	kind := reflectType.Kind()
	if kind == reflect.Ptr {
		ret.canBeNull = true
		reflectType = removeIndirection(reflectType)
		kind = reflectType.Kind()
	}

	// As we build up the chain of tsType -> tsType that fully describes a type
	// we come across named type. For example: map[string]Donut, where Donut
	// could be a "type Donut struct {...}", or a type based on a primitive
	// type, such as "type Donut string". In this case we need to add that type
	// to all of our known types and return a reference to that type from here.
	if !calledFromAddType && // This codepath should only kick in when we are in the middle of adding a complex type, not when we've been called directly by addType().
		reflectType.Name() != "" && // Don't bother with anonymous structs.
		!isTime(reflectType) && // Also skip time.Time.
		(!isPrimitive(reflectType.Kind()) ||
			(isPrimitive(reflectType.Kind()) && reflectType.Name() != reflectType.Kind().String())) { // Avoids the case where a string shows up with a name of "string" and a kind of "string".
		typeName, err := g.addType(reflectType, "")
		if err == nil {
			return &tsType{
				typeName:  typeName,
				canBeNull: ret.canBeNull,
			}
		}
	}

	// Default to setting the tsType from the Go type.
	if !isPrimitive(reflectType.Kind()) {
		nativeType := reflectType.String()
		if i := strings.IndexByte(nativeType, '.'); i > -1 {
			nativeType = nativeType[i+1:]
		}
		ret.typeName = nativeType
	}

	// Update the type if the kind points to something besides a primitive type.
	switch kind {
	case reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uint,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Int,
		reflect.Float32,
		reflect.Float64:
		ret.typeName = "number"
	case reflect.String:
		ret.typeName = "string"
	case reflect.Bool:
		ret.typeName = "boolean"
	case reflect.Map:
		ret.typeName = "map"
		keyTSTtype := g.tsTypeFromReflectType(reflectType.Key(), false)
		ret.keyType = keyTSTtype.typeName
		ret.subType = g.tsTypeFromReflectType(reflectType.Elem(), false)

	case reflect.Slice, reflect.Array:
		ret.typeName = "array"
		ret.canBeNull = (kind == reflect.Slice)
		ret.subType = g.tsTypeFromReflectType(reflectType.Elem(), false)

	case reflect.Struct:
		if isTime(reflectType) {
			ret.typeName = "string"
		} else {
			ret.typeName = "interface"
			name, _ := g.addType(reflectType, "")
			ret.interfaceName = name
		}

	case reflect.Interface:
		ret.typeName = "any"
	}
	return &ret
}

func (g *Go2TS) addTypeFields(st *structRep, reflectType reflect.Type) {
	for i := 0; i < reflectType.NumField(); i++ {
		structField := reflectType.Field(i)

		// Skip unexported fields.
		if len(structField.Name) == 0 || !ast.IsExported(structField.Name) {
			continue
		}

		structFieldType := structField.Type
		field := newFieldRep(structField)
		field.tsType = g.tsTypeFromReflectType(structFieldType, false)
		st.Fields = append(st.Fields, field)
	}
}

func (g *Go2TS) getAnonymousInterfaceName() string {
	g.anonymousCount++
	return fmt.Sprintf("Anonymous%d", g.anonymousCount)
}

func (g *Go2TS) addType(reflectType reflect.Type, interfaceName string) (string, error) {
	reflectType = removeIndirection(reflectType)
	if tsTypeName, ok := g.seen[reflectType]; ok {
		return tsTypeName, nil
	}

	if reflectType.Kind() == reflect.Struct {
		if interfaceName == "" {
			interfaceName = strings.Title(reflectType.Name())
		}
		if interfaceName == "" {
			interfaceName = g.getAnonymousInterfaceName()
		}

		st := structRep{
			Name:   interfaceName,
			Fields: make([]fieldRep, 0, reflectType.NumField()),
		}

		g.seen[reflectType] = st.Name
		g.addTypeFields(&st, reflectType)
		g.structs = append(g.structs, st)
		return st.Name, nil
	}

	// Handle non-struct types.
	topLevelTSType := newTopLevelTSType(reflectType)
	topLevelTSType.tsType = g.tsTypeFromReflectType(reflectType, true)
	g.seen[reflectType] = topLevelTSType.name
	g.nonStructs = append(g.nonStructs, topLevelTSType)
	return topLevelTSType.name, nil
}

// tsType represents either a type of a field, like "string", or part of
// a more complex type like the map[string] part of map[string]time.Time.
type tsType struct {
	// typeName is the TypeScript type, such as "string", or "map", or "SomeInterfaceName".
	typeName string

	// keyType is the type of the key if this tsType is a map, such as "string" or "number".
	keyType string

	// interfaceName is the name of the TypeScript interface if the tsType is
	// "interface".
	interfaceName string

	canBeNull bool
	subType   *tsType
}

// String returns the tsType formatted as TypeScript.
func (s tsType) String() string {
	var ret string
	switch s.typeName {
	case "array":
		ret = s.subType.String() + "[]"
	case "map":
		ret = fmt.Sprintf("{ [key: %s]: %s }", s.keyType, s.subType.String())
	case "interface":
		ret = s.interfaceName
	default:
		ret = s.typeName
	}

	return ret
}

type topLevelTSType struct {
	name   string
	tsType *tsType
}

func (t topLevelTSType) String() string {
	return fmt.Sprintf("\nexport type %s = %s;", t.name, t.tsType.String())
}

// newTopLevelTSType creates a new topLevelTSType from the given reflect.Type.
func newTopLevelTSType(reflectType reflect.Type) topLevelTSType {
	return topLevelTSType{
		name: reflectType.Name(),
	}
}

// fieldRep represents one field in a struct.
type fieldRep struct {
	// The name of the interface field.
	name       string
	tsType     *tsType
	isOptional bool
}

func (f fieldRep) String() string {
	optional := ""
	if f.isOptional {
		optional = "?"
	}

	canBeNull := ""
	if f.tsType.canBeNull {
		canBeNull = " | null"
	}

	return fmt.Sprintf("\t%s%s: %s%s;", f.name, optional, f.tsType.String(), canBeNull)
}

// newFieldRep creates a new fieldRep from the given reflect.StructField.
func newFieldRep(structField reflect.StructField) fieldRep {
	var ret fieldRep
	jsonTag := strings.Split(structField.Tag.Get("json"), ",")

	ret.name = structField.Name
	if len(jsonTag) > 0 && jsonTag[0] != "" {
		ret.name = jsonTag[0]
	}
	ret.isOptional = len(jsonTag) > 1 && jsonTag[1] == "omitempty"
	return ret
}

func isTime(t reflect.Type) bool {
	return t.Name() == "Time" && t.PkgPath() == "time"
}

// structRep represents a single Go struct.
type structRep struct {
	// Name is the TypeScript interface Name in the generated output.
	Name   string
	Fields []fieldRep
}

var structRepTemplate = template.Must(template.New("").Parse(`
export interface {{ .Name }} {
{{ range .Fields -}}
	{{- . }}
{{ end -}}
}
`))

func (s *structRep) render(w io.Writer) error {
	return structRepTemplate.Execute(w, s)
}

func removeIndirection(reflectType reflect.Type) reflect.Type {
	kind := reflectType.Kind()
	// Follow all the pointers until we get to a non-Ptr kind.
	for kind == reflect.Ptr {
		reflectType = reflectType.Elem()
		kind = reflectType.Kind()
	}
	return reflectType
}
