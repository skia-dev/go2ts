// Package go2ts is an extremely simple and powerful Go to TypeScript generator.
// It can handle all JSON serializable Go types and also has the ability to
// define TypeScript union types for your enum-like types.
package go2ts

import (
	"fmt"
	"go/ast"
	"io"
	"reflect"
	"strings"

	"github.com/skia-dev/go2ts/typescript"
)

// Go2TS writes TypeScript definitions for Go types.
type Go2TS struct {
	// typeDeclarations maps any added reflect.Types to their corresponding TypeScript type
	// declarations.
	typeDeclarations map[reflect.Type]typescript.TypeDeclaration

	// typeDeclarationsInOrder holds type declarations in the order they were added, which determines
	// the order they will appear in the output TypeScript code. This field and typeDeclarations
	// should be kept in sync.
	typeDeclarationsInOrder []typescript.TypeDeclaration

	// anonymousCount keeps track of the number of anonymous structs we've had to name.
	anonymousCount int
}

// New returns a new *Go2TS.
func New() *Go2TS {
	ret := &Go2TS{
		typeDeclarations:        map[reflect.Type]typescript.TypeDeclaration{},
		typeDeclarationsInOrder: []typescript.TypeDeclaration{},
	}
	return ret
}

func (g *Go2TS) getOrSaveTypeDeclaration(reflectType reflect.Type, typeDeclaration typescript.TypeDeclaration) typescript.TypeDeclaration {
	if existingTypeDeclaration, ok := g.typeDeclarations[reflectType]; ok {
		return existingTypeDeclaration
	} else {
		g.typeDeclarations[reflectType] = typeDeclaration
		g.typeDeclarationsInOrder = append(g.typeDeclarationsInOrder, typeDeclaration)
		return typeDeclaration
	}
}

// Add a type that needs a TypeScript definition.
//
// See AddToNamespace() for more details.
func (g *Go2TS) Add(v interface{}) error {
	return g.AddToNamespace(v, "")
}

// AddToNamespace adds a type that needs a TypeScript definition to the given TypeScript namespace.
//
// See AddWithNameToNamespace() for more details.
func (g *Go2TS) AddToNamespace(v interface{}, namespace string) error {
	return g.AddWithNameToNamespace(v, "", namespace)
}

// AddMultiple adds multiple types in a single call.
//
// See AddMultipleToNamespace() for more details.
func (g *Go2TS) AddMultiple(values ...interface{}) error {
	return g.AddMultipleToNamespace("", values...)
}

// AddMultipleToNamespace adds multiple types to the given TypeScript namespace in a single call.
//
// Will stop at the first type that fails.
func (g *Go2TS) AddMultipleToNamespace(namespace string, values ...interface{}) error {
	for _, v := range values {
		if err := g.AddToNamespace(v, namespace); err != nil {
			return err
		}
	}
	return nil
}

// AddWithName adds a type that needs a TypeScript definition.
//
// See AddWithNameToNamespace() for more details.
func (g *Go2TS) AddWithName(v interface{}, interfaceName string) error {
	return g.AddWithNameToNamespace(v, interfaceName, "")
}

// AddWithNameToNamespace adds a type that needs a TypeScript definition.
//
// The value passed in can be an instance of a type, a reflect.Type, or a
// reflect.Value.
//
// The 'name' supplied will be the TypeScript interface name. If 'interfaceName'
// is the empty string then the Go type name will be used. If the type is of a
// struct that is anonymous it will be given a name of the form "AnonymousN".
//
// If the type is a struct, the fields of the struct will be named following the
// convention for json serialization, including using the json tag if supplied.
// Fields tagged with `json:",omitempty"` will be marked as optional.
//
// There is special handling of time.Time types to be TypeScript "string"s since
// time.Time implements MarshalJSON, see
// https://pkg.go.dev/time?tab=doc#Time.MarshalJSON.
//
// If namespace is non-empty, the type will be added inside a TypeScript
// namespace of that name.
func (g *Go2TS) AddWithNameToNamespace(v interface{}, interfaceName, namespace string) error {
	var reflectType reflect.Type
	switch v := v.(type) {
	case reflect.Type:
		reflectType = v
	case reflect.Value:
		reflectType = v.Type()
	default:
		reflectType = reflect.TypeOf(v)
	}

	g.addTypeDeclaration(reflectType, interfaceName, namespace)
	return nil
}

// AddUnion adds a TypeScript definition for a union type of the values in 'v',
// which must be a slice or an array.
//
// See AddUnionToNamespace() for more details.
func (g *Go2TS) AddUnion(v interface{}) error {
	return g.AddUnionToNamespace(v, "")
}

// AddUnionToNamespace adds a TypeScript definition for a union type of the values in 'v',
// which must be a slice or an array, to the given TypeScript namespace.
//
// See AddUnionWithNameToNamespace() for more details.
func (g *Go2TS) AddUnionToNamespace(v interface{}, namespace string) error {
	return g.AddUnionWithNameToNamespace(v, "", namespace)
}

// AddMultipleUnion adds multple union types.
//
// See AddMultipleUnionToNamespace() for more details.
func (g *Go2TS) AddMultipleUnion(values ...interface{}) error {
	return g.AddMultipleUnionToNamespace("", values...)
}

// AddMultipleUnionToNamespace adds multple union types to the given TypeScript namespace.
//
// Will stop at the first union type that fails.
func (g *Go2TS) AddMultipleUnionToNamespace(namespace string, values ...interface{}) error {
	for _, v := range values {
		if err := g.AddUnionToNamespace(v, namespace); err != nil {
			return err
		}
	}
	return nil
}

// AddUnionWithName adds a TypeScript definition for a union type of the values
// in 'v', which must be a slice or an array.
//
// See AddUnionWithNameToNamespace() for more details.
//
func (g *Go2TS) AddUnionWithName(v interface{}, typeName string) error {
	return g.AddUnionWithNameToNamespace(v, typeName, "")
}

// AddUnionWithNameToNamespace adds a TypeScript definition for a union type of
// the values in 'v', which must be a slice or an array, to the given namespace.
//
// If typeName is the empty string then the name of type of elements in the
// slice or array is used as the type name, otherwise the typeName supplied will
// be used as the TypeScript type name.
func (g *Go2TS) AddUnionWithNameToNamespace(v interface{}, typeName, namespace string) error {
	// We can only build union types from Go slices or arrays.
	reflectType := reflect.TypeOf(v)
	if reflectType.Kind() != reflect.Slice && reflectType.Kind() != reflect.Array {
		return fmt.Errorf("AddUnionWithName must be supplied an array or slice, got %v: %v", reflectType.Kind(), v)
	}

	// Make sure we have a name for the union type.
	if typeName == "" {
		typeName = reflectType.Elem().Name()
	}

	// We will populate the union type with the typescript.LiteralTypes corresponding to the elements
	// in the passed in Go slice.
	unionType := &typescript.UnionType{
		Types: []typescript.Type{},
	}

	// Iterate over all elements in the passed in Go slice.
	values := reflect.ValueOf(v)
	for i := 0; i < values.Len(); i++ {
		value := values.Index(i)

		// Obtain the typescript.BasicType corresponding to the current element. We only support basic
		// types; any other types will result in a panic.
		var basicType typescript.BasicType
		if value.Kind() == reflect.Bool {
			basicType = typescript.Boolean
		} else if isNumber(value.Kind()) {
			basicType = typescript.Number
		} else if value.Kind() == reflect.String {
			basicType = typescript.String
		} else {
			return fmt.Errorf("Go Kind %q cannot be used in a TypeScript union type.", value.Kind())
		}

		// Create a typescript.LiteralType for the current element and add it to the union type.
		unionType.Types = append(unionType.Types, &typescript.LiteralType{
			BasicType: basicType,
			Literal:   fmt.Sprintf("%v", values.Index(i).Interface()),
		})
	}

	if existingTypeDeclaration, ok := g.typeDeclarations[reflectType.Elem()]; ok {
		// The reflect.Type was already added, so if it's a TypeScript type alias, we'll update it to be
		// an alias for the newly added union type.
		existingTypeAliasDeclaration, ok := existingTypeDeclaration.(*typescript.TypeAliasDeclaration)
		if !ok {
			return fmt.Errorf("Go type %v was already added as something other than a TypeScript type alias.", reflectType.Elem())
		}
		existingTypeAliasDeclaration.Namespace = namespace
		existingTypeAliasDeclaration.Identifier = typeName
		existingTypeAliasDeclaration.Type = unionType
	} else {
		// The reflect.Type hasn't been seen before, so we declare a new type alias for the union type.
		g.getOrSaveTypeDeclaration(reflectType.Elem(), &typescript.TypeAliasDeclaration{
			Namespace:  namespace,
			Identifier: typeName,
			Type:       unionType,
		})
	}

	return nil
}

// Render the TypeScript definitions to the given io.Writer.
func (g *Go2TS) Render(w io.Writer) error {
	_, err := fmt.Fprintln(w, "// DO NOT EDIT. This file is automatically generated.")
	if err != nil {
		return err
	}

	// Output TypeScript interfaces first.
	for _, typeDeclaration := range g.typeDeclarationsInOrder {
		if _, ok := typeDeclaration.(*typescript.InterfaceDeclaration); !ok {
			continue
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, typeDeclaration.ToTypeScript())
	}

	// Output any other type definitions (e.g. type aliases) second.
	for _, typeDeclaration := range g.typeDeclarationsInOrder {
		if _, ok := typeDeclaration.(*typescript.InterfaceDeclaration); ok {
			continue
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, typeDeclaration.ToTypeScript())
	}

	return nil
}

// numbers is the set of Kinds that we convert into the TypeScript "number" type.
var numberKinds = map[reflect.Kind]bool{
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
	reflect.Float32: true,
	reflect.Float64: true,
}

func isNumber(kind reflect.Kind) bool {
	return numberKinds[kind]
}

// nonNumberPrimitiveKinds is the non-number set of Kinds that we support converting to TypeScript.
var nonNumberPrimitiveKinds = map[reflect.Kind]bool{
	reflect.Bool:    true,
	reflect.Uintptr: true,
	reflect.String:  true,
}

func isPrimitive(kind reflect.Kind) bool {
	return numberKinds[kind] || nonNumberPrimitiveKinds[kind]
}

func isPrimitiveAlias(reflectType reflect.Type) bool {
	return isPrimitive(reflectType.Kind()) && reflectType.Name() != reflectType.Kind().String()
}

func (g *Go2TS) reflectTypeToTypeScriptType(reflectType reflect.Type, namespace string, wasExplicitlyAdded, ignoreNil bool) typescript.Type {
	// If the type is a pointer, then we remove the pointer indirection, compute the resulting
	// TypeScript type, and return the union between that type and null.
	if reflectType.Kind() == reflect.Ptr {
		tsType := g.reflectTypeToTypeScriptType(removeIndirection(reflectType), namespace, wasExplicitlyAdded, ignoreNil)
		if ignoreNil {
			return tsType
		}
		return &typescript.UnionType{
			Types: []typescript.Type{tsType, typescript.Null},
		}
	}

	// If we have declared this type before, then we just return a reference to the declared type.
	if existingTypeDeclaration, ok := g.typeDeclarations[reflectType]; ok {
		return existingTypeDeclaration.TypeReference()
	}

	// Structs are declared as interfaces (save for time.Time, which is a special case handled below).
	if reflectType.Kind() == reflect.Struct && !isTime(reflectType) {
		return g.addInterfaceDeclaration(reflectType, "", namespace).TypeReference()
	}

	// Will hold the typescript.Type extracted from the reflect.Type.
	var tsType typescript.Type

	// Compute the TypeScript type based on the Kind of the reflected type.
	switch reflectType.Kind() {
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
		tsType = typescript.Number

	case reflect.String:
		tsType = typescript.String

	case reflect.Bool:
		tsType = typescript.Boolean

	case reflect.Map:
		// TypeScript index signature parameter types[1] must be either "string" or "number", and
		// cannot be type aliases, otherwise the TypeScript compiler will fail with error
		// "TS1336: An index signature parameter type cannot be a type alias.".
		//
		// Example:
		//
		//   export type Foo = string;
		//   export type Bar = { [key: Foo]: string };  // Compiler produces error TS1336.
		//
		// Thus, we treat map keys as a special case where we ignore any type aliases and use either
		// "string" or "number" directly.
		//
		// [1] https://www.typescriptlang.org/docs/handbook/advanced-types.html#index-types-and-index-signatures.
		var indexType typescript.Type
		if reflectType.Key().Kind() == reflect.String {
			indexType = typescript.String
		} else if isNumber(reflectType.Key().Kind()) {
			indexType = typescript.Number
		} else {
			panic(fmt.Sprintf("Go Kind %q cannot be used as a TypeScript index signature parameter type.", reflectType.Key().Kind()))
		}

		tsType = &typescript.MapType{
			IndexType: indexType,
			ValueType: g.reflectTypeToTypeScriptType(reflectType.Elem(), namespace, false /* =wasExplicitlyAdded */, ignoreNil),
		}

	case reflect.Slice, reflect.Array:
		tsType = &typescript.ArrayType{
			ItemsType: g.reflectTypeToTypeScriptType(reflectType.Elem(), namespace, false /* =wasExplicitlyAdded */, ignoreNil),
		}
		// Slices can be nil, but not arrays.
		if reflectType.Kind() == reflect.Slice && !ignoreNil {
			tsType = &typescript.UnionType{
				Types: []typescript.Type{tsType, typescript.Null},
			}
		}

	case reflect.Struct:
		// This is necessarily a time.Time because we handled all other structs earlier.
		tsType = typescript.String

	case reflect.Interface:
		tsType = typescript.Any

	case reflect.Complex64,
		reflect.Complex128,
		reflect.Chan,
		reflect.Func,
		reflect.UnsafePointer:
		panic(fmt.Sprintf("Go Kind %q cannot be serialized to JSON.", reflectType.Kind()))
	}

	// If this is a named Go type (e.g. "Donut", assuming we have added a "type Donut string" Go type)
	// then we want to declare it as a TypeScript type alias (e.g. "export type Donut = string"), and
	// this function should return a reference to the alias (e.g "Donut") instead of the underlying
	// type (e.g. "string").
	//
	// Note that we only want to do this if the type in question wasn't added explicitly via a call to
	// one of the Go2TS.Add*() methods because said methods will add the type declarations themselves.
	if !wasExplicitlyAdded &&
		// All type aliases have a non-empty name.
		reflectType.Name() != "" &&
		// But not all types with non-empty names are aliases (e.g. the name for the int type is "int").
		(!isPrimitive(reflectType.Kind()) || isPrimitiveAlias(reflectType)) &&
		// We don't want an alias for time.Time because we treat it as a string in TypeScript.
		!isTime(reflectType) {
		typeDeclaration := &typescript.TypeAliasDeclaration{
			Namespace:  namespace,
			Identifier: reflectType.Name(),
			Type:       tsType,
		}

		// If we've already added a TypeScript type delcaration for this Go type, we'll return a
		// reference to the existing declaration, otherwise we'll return a reference to the new
		// declaration.
		return g.getOrSaveTypeDeclaration(reflectType, typeDeclaration).TypeReference()
	}

	return tsType
}

// populateInterfaceDeclarationProperties recursively populates the properties of the given
// interface declaration. It assumes reflectType's Kind is Struct. If recursivelyForceOptional is
// true, any properties populated on this or any recursive calls to this method will be marked as
// optional.
func (g *Go2TS) populateInterfaceDeclarationProperties(interfaceDeclaration *typescript.InterfaceDeclaration, structType reflect.Type, recursivelyForceOptional bool) {
	// Iterate over all fields of the given struct.
	for i := 0; i < structType.NumField(); i++ {
		structField := structType.Field(i)

		// Skip unexported fields.
		if len(structField.Name) == 0 || !ast.IsExported(structField.Name) {
			continue
		}

		// If the field is an embedded struct, or an embedded struct pointer, we add the inner struct's
		// fields to the outer struct (i.e. we flatten the structs). This is consistent with
		// json.Marshal().
		if structField.Anonymous && removeIndirection(structField.Type).Kind() == reflect.Struct {
			// If the field is an embedded struct pointer, we recursively mark all its fields as optional.
			// This is because json.Marshal() will omit said fields if the embedded struct pointer is nil.
			g.populateInterfaceDeclarationProperties(interfaceDeclaration, removeIndirection(structField.Type), recursivelyForceOptional || structField.Type.Kind() == reflect.Ptr)
			continue
		}

		// Read the field's `json:...` tag.
		jsonTag := strings.Split(structField.Tag.Get("json"), ",")

		// Read the property name from the `json:...` tag, or default to the field name.
		propertyName := structField.Name
		if len(jsonTag) > 0 && jsonTag[0] != "" {
			propertyName = jsonTag[0]
		}

		// A `json:"-"` tag means the field will not be serialized to JSON, so we can skip it.
		if propertyName == "-" {
			continue
		}

		// If a field in an embedded struct has the same name as a field in the outer struct, the
		// outermost field will take precendence in the output of json.Marshal(). However, this opaque
		// behavior is probably not what the programmer intended, so we fail loudly to prevent bugs.
		for _, property := range interfaceDeclaration.Properties {
			if propertyName == property.Identifier {
				panic(fmt.Sprintf("Attempted to populate interface %q with more than one field named %q. (Did you embed two structs with overlapping field names?)", interfaceDeclaration.Identifier, property.Identifier))
			}
		}

		// A `go2ts:"ignorenil"` tag means that any nillable types will be treated as their non-nillable
		// counterparts when recursively computing the TypeScript type of the current field. Concretely,
		// this means that pointers will have the indirection removed, and slices will be treated as
		// arrays.
		//
		// Note that "ignorenil" propagates recursively, meaning that any previously unseen types will
		// be added with their nil types ignored. For example, if the current field has type Foo,
		// defined as "type Foo []string", and it's annotated with `go2ts:"ignorenil"`, then the
		// TypeScript type Foo will be declared as "type Foo = string[]" instead of
		// "type Foo = string[] | null".
		ignoreNil := structField.Tag.Get("go2ts") == "ignorenil"

		// Recursively compute the property's TypeScript type.
		propertyType := g.reflectTypeToTypeScriptType(structField.Type, interfaceDeclaration.Namespace, false /* =wasExplicitlyAdded */, ignoreNil)

		// We mark the property as optional if the field is tagged with "omitempty".
		markedAsOptional := len(jsonTag) > 1 && jsonTag[1] == "omitempty"

		// Create the property signature and add it to the interface declaration.
		property := typescript.PropertySignature{
			Identifier: propertyName,
			Type:       propertyType,
			Optional:   recursivelyForceOptional || markedAsOptional,
		}
		interfaceDeclaration.Properties = append(interfaceDeclaration.Properties, property)
	}
}

func (g *Go2TS) getAnonymousInterfaceName() string {
	g.anonymousCount++
	return fmt.Sprintf("Anonymous%d", g.anonymousCount)
}

func (g *Go2TS) addInterfaceDeclaration(structType reflect.Type, interfaceName, namespace string) *typescript.InterfaceDeclaration {
	structType = removeIndirection(structType)

	// Only structs can be declared as TypeScript interfaces.
	if structType.Kind() != reflect.Struct {
		panic(fmt.Sprintf(`Go Kind %q cannot be declared as a TypeScript interface.`, structType.Kind()))
	}

	// Nothing to do if the TypeScript interface has already been declared.
	if existingTypeDeclaration, ok := g.typeDeclarations[structType]; ok {
		return existingTypeDeclaration.(*typescript.InterfaceDeclaration)
	}

	// Make sure we have a name for the interface, which could be anonymous.
	if interfaceName == "" {
		interfaceName = strings.Title(structType.Name())
	}
	if interfaceName == "" {
		interfaceName = g.getAnonymousInterfaceName()
	}

	// Create the interface declaration and populate its fields, which recurses into any embedded
	// structs.
	interfaceDeclaration := &typescript.InterfaceDeclaration{
		Namespace:  namespace,
		Identifier: interfaceName,
		Properties: []typescript.PropertySignature{},
	}
	g.populateInterfaceDeclarationProperties(interfaceDeclaration, structType, false /* =recursivelyForceOptional */)

	g.typeDeclarations[structType] = interfaceDeclaration
	g.typeDeclarationsInOrder = append(g.typeDeclarationsInOrder, interfaceDeclaration)

	return interfaceDeclaration
}

func (g *Go2TS) addTypeDeclaration(reflectType reflect.Type, typeName, namespace string) {
	// Struct types are declared as TypeScript interfaces.
	if removeIndirection(reflectType).Kind() == reflect.Struct {
		g.addInterfaceDeclaration(reflectType, typeName, namespace)
		return
	}

	// All other type declarations are handled as type aliases (except for union types, which are
	// handled separately).
	if _, ok := g.typeDeclarations[reflectType]; ok {
		return
	}

	if typeName == "" {
		typeName = reflectType.Name()
	}
	typeDeclaration := &typescript.TypeAliasDeclaration{
		Namespace:  namespace,
		Identifier: typeName,
		Type:       g.reflectTypeToTypeScriptType(reflectType, namespace, true /* =wasExplicitlyAdded */, false /* =ignoreNil */),
	}

	g.getOrSaveTypeDeclaration(reflectType, typeDeclaration)
}

func isTime(t reflect.Type) bool {
	return t.Name() == "Time" && t.PkgPath() == "time"
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
