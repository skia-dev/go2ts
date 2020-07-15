package go2ts

import (
	"bytes"
	"image/color"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender_ComplexStruct_Success(t *testing.T) {

	type OtherStruct struct {
		T time.Time `json:"t,omitempty"`
	}

	type Mode string

	type Offset int

	type Direction string

	const (
		Up    Direction = "up"
		Down  Direction = "down"
		Left  Direction = "left"
		Right Direction = "right"
	)

	var AllDirections = []Direction{Up, Down, Left, Right}

	type Data map[string]interface{}

	type ComplexStruct struct {
		String                 string
		StringWithAnnotation   string `json:"s"`
		Bool                   bool
		Int                    int
		Float64                float64
		Time                   time.Time
		Other                  OtherStruct
		OtherPtr               *OtherStruct
		OptionalString         string       `json:",omitempty"`
		OptionalInt            int          `json:",omitempty"`
		OptionalFloat64        float64      `json:",omitempty"`
		OptionalTime           time.Time    `json:",omitempty"`
		OptionalOtherStruct    OtherStruct  `json:",omitempty"`
		OptionalOtherStructPtr *OtherStruct `json:",omitempty"`
		Data                   Data
		DataPtr                *Data
		MapStringSlice         map[string][]string
		MapStringSliceSlice    map[string][][]string
		MapStringPtrSlice      map[string][]*string
		MapIntKeys             map[int]string
		MapStringAliasKeys     map[Mode]string
		MapIntAliasKeys        map[Offset]string
		MapOtherStruct         map[string]OtherStruct
		MapOtherStructPtr      map[string]*OtherStruct
		Slice                  []string
		SliceOfSlice           [][]string
		SliceOfData            []Data
		MapOfData              map[string]Data
		MapOfSliceOfData       map[string][]Data
		MapOfMapOfSliceOfData  map[string]map[string][]Data
		Mode                   Mode
		InlineStruct           struct{ A int }
		Array                  [3]string
		skipped                bool
		Offset                 Offset
		Color                  color.Alpha
		Direction              Direction
		NotSerialized          string `json:"-"`
	}

	const complexStructExpected = `// DO NOT EDIT. This file is automatically generated.

export interface OtherStruct {
	t?: string;
}

export interface Anonymous1 {
	A: number;
}

export interface Alpha {
	A: number;
}

export interface ComplexStruct {
	String: string;
	s: string;
	Bool: boolean;
	Int: number;
	Float64: number;
	Time: string;
	Other: OtherStruct;
	OtherPtr: OtherStruct | null;
	OptionalString?: string;
	OptionalInt?: number;
	OptionalFloat64?: number;
	OptionalTime?: string;
	OptionalOtherStruct?: OtherStruct;
	OptionalOtherStructPtr?: OtherStruct | null;
	Data: Data;
	DataPtr: Data | null;
	MapStringSlice: { [key: string]: string[] | null };
	MapStringSliceSlice: { [key: string]: (string[] | null)[] | null };
	MapStringPtrSlice: { [key: string]: (string | null)[] | null };
	MapIntKeys: { [key: number]: string };
	MapStringAliasKeys: { [key: string]: string };
	MapIntAliasKeys: { [key: number]: string };
	MapOtherStruct: { [key: string]: OtherStruct };
	MapOtherStructPtr: { [key: string]: OtherStruct | null };
	Slice: string[] | null;
	SliceOfSlice: (string[] | null)[] | null;
	SliceOfData: Data[] | null;
	MapOfData: { [key: string]: Data };
	MapOfSliceOfData: { [key: string]: Data[] | null };
	MapOfMapOfSliceOfData: { [key: string]: { [key: string]: Data[] | null } };
	Mode: Mode;
	InlineStruct: Anonymous1;
	Array: string[];
	Offset: Offset;
	Color: Alpha;
	Direction: Direction;
}

export type Data = { [key: string]: any };

export type Mode = string;

export type Offset = number;

export type Direction = "up" | "down" | "left" | "right";
`

	go2ts := New()
	err := go2ts.Add(ComplexStruct{})
	require.NoError(t, err)
	err = go2ts.AddUnion(AllDirections)
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	assert.Equal(t, complexStructExpected, b.String())
}

func TestRender_NoTypesAdded_ReturnsEmptyString(t *testing.T) {
	go2ts := New()
	var b bytes.Buffer
	err := go2ts.Render(&b)
	require.NoError(t, err)
	assert.Equal(t, "// DO NOT EDIT. This file is automatically generated.\n", b.String())
}

func TestRender_SameTypeAddedInMultipleWays_RendersTypeOnce(t *testing.T) {
	type SomeStruct struct {
		B string
	}

	go2ts := New()
	err := go2ts.Add(reflect.TypeOf(SomeStruct{}))
	require.NoError(t, err)
	err = go2ts.AddWithName(SomeStruct{}, "ADifferentName")
	require.NoError(t, err)
	err = go2ts.Add(reflect.TypeOf([]SomeStruct{}).Elem())
	require.NoError(t, err)
	err = go2ts.Add(SomeStruct{})
	require.NoError(t, err)
	err = go2ts.Add(&SomeStruct{})
	require.NoError(t, err)
	err = go2ts.Add(reflect.New(reflect.TypeOf(SomeStruct{})))
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export interface SomeStruct {
	B: string;
}
`
	assert.Equal(t, expected, b.String())
}

func TestRender_FirstAddDeterminesInterfaceName(t *testing.T) {
	type SomeStruct struct {
		B string
	}

	go2ts := New()
	err := go2ts.AddWithName(SomeStruct{}, "ADifferentName")
	require.NoError(t, err)
	err = go2ts.Add(reflect.TypeOf(SomeStruct{}))
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export interface ADifferentName {
	B: string;
}
`
	assert.Equal(t, expected, b.String())
}

func TestRender_NonStructTypes_Success(t *testing.T) {
	type Data map[string]interface{}

	go2ts := New()
	err := go2ts.Add(Data{})
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export type Data = { [key: string]: any };
`
	assert.Equal(t, expected, b.String())
}

func TestRender_NonStructTypeWithName_Success(t *testing.T) {
	type Data map[string]interface{}

	go2ts := New()
	err := go2ts.AddWithName(Data{}, "SomeNewName")
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export type SomeNewName = { [key: string]: any };
`
	assert.Equal(t, expected, b.String())
}

type HasUnsupportedFieldTypes struct {
	C complex128
}

func TestAdd_UnsupportedType_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			assert.Fail(t, "Complex128 is unsupported and should panic.")
		}
	}()

	go2ts := New()
	err := go2ts.Add(HasUnsupportedFieldTypes{})
	require.NoError(t, err)
}

func TestAddUnionWithName_SliceOfString_Success(t *testing.T) {
	type DayOfWeek string

	const (
		Monday  DayOfWeek = "Mon"
		Tuesday DayOfWeek = "Tue"
	)

	go2ts := New()
	err := go2ts.AddUnionWithName([]DayOfWeek{Monday, Tuesday}, "")
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export type DayOfWeek = "Mon" | "Tue";
`
	assert.Equal(t, expected, b.String())
}

func TestAddUnion_SliceOfString_Success(t *testing.T) {
	type DayOfWeek string

	const (
		Monday  DayOfWeek = "Mon"
		Tuesday DayOfWeek = "Tue"
	)

	go2ts := New()
	err := go2ts.AddUnion([]DayOfWeek{Monday, Tuesday})
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export type DayOfWeek = "Mon" | "Tue";
`
	assert.Equal(t, expected, b.String())
}

func TestAddUnionWithName_SliceOfStringWithName_Success(t *testing.T) {
	type DayOfWeek string

	const (
		Monday  DayOfWeek = "Mon"
		Tuesday DayOfWeek = "Tue"
	)

	go2ts := New()
	err := go2ts.AddUnionWithName([]DayOfWeek{Monday, Tuesday}, "ShouldBeTheTypeName")
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export type ShouldBeTheTypeName = "Mon" | "Tue";
`
	assert.Equal(t, expected, b.String())
}

func TestAddUnionWithName_ArrayOfInt_Success(t *testing.T) {
	type SomeOption int

	const (
		OptionA SomeOption = 1
		OptionB SomeOption = 3
	)

	go2ts := New()
	err := go2ts.AddUnionWithName([2]SomeOption{OptionA, OptionB}, "")
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export type SomeOption = 1 | 3;
`
	assert.Equal(t, expected, b.String())
}

func TestAddUnionWithName_NotSliceOrArray_ReturnsError(t *testing.T) {
	type SomeOption int

	go2ts := New()
	err := go2ts.AddUnionWithName(SomeOption(1), "")
	require.Error(t, err)
}

func TestAddUnion_DefinitionFoundFromStructAndUnion_UnionTypeDefinitionIsEmitted(t *testing.T) {
	type SomeOption int

	const (
		OptionA SomeOption = 1
		OptionB SomeOption = 3
	)

	type SomeStruct struct {
		Choices SomeOption
	}

	go2ts := New()
	err := go2ts.Add(SomeStruct{})
	require.NoError(t, err)
	err = go2ts.AddUnion([2]SomeOption{OptionA, OptionB})
	require.NoError(t, err)
	var b bytes.Buffer
	err = go2ts.Render(&b)
	require.NoError(t, err)
	expected := `// DO NOT EDIT. This file is automatically generated.

export interface SomeStruct {
	Choices: SomeOption;
}

export type SomeOption = 1 | 3;
`
	assert.Equal(t, expected, b.String())
}
