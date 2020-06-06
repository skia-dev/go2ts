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
	interfaces []interfaceRep
	types      []topLevelTSType

	// seen maps from a reflect.Type to a TypeScript type name.
	seen map[reflect.Type]string

	// anonymousCount keeps track of the number of anonymous structs we've had to name.
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
	for _, intf := range g.interfaces {
		if err := intf.render(w); err != nil {
			return err
		}
	}

	for _, typ := range g.types {
		if _, err := fmt.Fprintln(w, typ.String()); err != nil {
			return err
		}
	}

	return nil
}

// The list if primitive types that we support converting to TypeScript.
var primitive = map[reflect.Kind]bool{
	reflect.Bool:    true,
	reflect.Int:     true,
	reflect.Int8:    true,
	reflect.Int16:   true,
	reflect.Int32:   true,
	reflect.Int64:   true,
	reflect.Uint:    true,
	reflect.Uint8:   true,
	reflect.Uint16:  true,
	reflect.Uint32:  true,
	reflect.Uint64:  true,
	reflect.Uintptr: true,
	reflect.Float32: true,
	reflect.Float64: true,
	reflect.String:  true,
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

	// As we build up the chain of tsTypes that fully describes a type we may come
	// across named type. For example: map[string]Donut, where Donut could be a
	// "type Donut struct {...}", or a type based on a primitive type, such as
	// "type Donut string". In this case we need to add that type to all of our
	// known types and return a reference to that type from here.
	if !calledFromAddType && // Don't do this if called from addType().
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

	// Default to using the Go lang type name as the TypeScript type name.
	nativeType := reflectType.String()

	// Strip off the module name of the native type if present.
	if i := strings.IndexByte(nativeType, '.'); i > -1 {
		nativeType = nativeType[i+1:]
	}
	ret.typeName = nativeType

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
		ret.keyType = g.tsTypeFromReflectType(reflectType.Key(), false)
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

	case reflect.Complex64,
		reflect.Complex128,
		reflect.Chan,
		reflect.Func,
		reflect.UnsafePointer:
		panic(fmt.Sprintf("Go kind %q can't be serialized to JSON.", kind))
	}
	return &ret
}

func (g *Go2TS) addTypeFields(st *interfaceRep, reflectType reflect.Type) {
	for i := 0; i < reflectType.NumField(); i++ {
		structField := reflectType.Field(i)

		// Skip unexported fields.
		if len(structField.Name) == 0 || !ast.IsExported(structField.Name) {
			continue
		}

		field := newFieldRep(structField)
		field.tsType = g.tsTypeFromReflectType(structField.Type, false)
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

		intf := interfaceRep{
			Name:   interfaceName,
			Fields: make([]fieldRep, 0, reflectType.NumField()),
		}

		g.seen[reflectType] = intf.Name
		g.addTypeFields(&intf, reflectType)
		g.interfaces = append(g.interfaces, intf)
		return intf.Name, nil
	}

	// Handle non-struct types.
	topLevelTSType := newTopLevelTSType(reflectType)
	topLevelTSType.tsType = g.tsTypeFromReflectType(reflectType, true)
	g.seen[reflectType] = topLevelTSType.name
	g.types = append(g.types, topLevelTSType)
	return topLevelTSType.name, nil
}

// tsType represents either a type of a field, like "string", or part of
// a more complex type like the map[string] part of map[string]time.Time.
type tsType struct {
	// typeName is the TypeScript type, such as "string", or "map", or "SomeInterfaceName".
	typeName string

	// keyType is the type of the key if this tsType is a map, such as "string" or "number".
	keyType *tsType

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
		ret = fmt.Sprintf("{ [key: %s]: %s }", s.keyType.String(), s.subType.String())
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

// interfaceRep represents a single Go struct that gets emitted as a TypeScript interface.
type interfaceRep struct {
	// Name is the TypeScript interface Name in the generated output.
	Name   string
	Fields []fieldRep
}

var interfaceRepTemplate = template.Must(template.New("").Parse(`
export interface {{ .Name }} {
{{ range .Fields -}}
	{{- . }}
{{ end -}}
}
`))

func (s *interfaceRep) render(w io.Writer) error {
	return interfaceRepTemplate.Execute(w, s)
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
