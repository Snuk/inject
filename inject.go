// Package inject provides utilities for mapping and injecting dependencies in various ways.
package inject

import (
	"fmt"
	"reflect"
)

// Injector represents an interface for mapping and injecting dependencies into structs
// and function arguments.
type Injector interface {
	Applicator
	Invoker
	TypeMapper
	// SetParent sets the parent of the injector. If the injector cannot find a
	// dependency in its Type map it will check its parent before returning an
	// error.
	SetParent(Injector)
}

// Applicator represents an interface for mapping dependencies to a struct.
type Applicator interface {
	// Maps dependencies in the Type map to each field in the struct
	// that is tagged with 'inject'. Returns an error if the injection
	// fails.
	Apply(interface{}) error
}

// Invoker represents an interface for calling functions via reflection.
type Invoker interface {
	// Invoke attempts to call the interface{} provided as a function,
	// providing dependencies for function arguments based on Type. Returns
	// a slice of reflect.Value representing the returned values of the function.
	// Returns an error if the injection fails.
	Invoke(interface{}) ([]reflect.Value, error)
}

// TypeMapper represents an interface for mapping interface{} values based on type.
type TypeMapper interface {
	// Maps the interface{} value based on its immediate type from reflect.TypeOf.
	Map(interface{}) TypeMapper
	// Maps the interface{} value based on the pointer of an Interface provided.
	// This is really only useful for mapping a value as an interface, as interfaces
	// cannot at this time be referenced directly without a pointer.
	MapTo(interface{}, interface{}) TypeMapper
	// Provide the dynamic type of interface{} returns,
	Provide(interface{}) TypeMapper
	// Provides a possibility to directly insert a mapping based on type and value.
	// This makes it possible to directly map type arguments not possible to instantiate
	// with reflect like unidirectional channels.
	Set(reflect.Type, reflect.Value) TypeMapper
	// Returns the Value that is mapped to the current type. Returns a zeroed Value if
	// the Type has not been mapped.
	Get(reflect.Type) reflect.Value
	// Returns all the Values that are mapped to the current interface. Returns an empty slice if
	// the Type has not been mapped.
	GetAll(reflect.Type) []reflect.Value
}

type instance struct {
	tp reflect.Type
	vl reflect.Value
}

type injector struct {
	values []instance
	parent Injector
}

// InterfaceOf dereferences a pointer to an Interface type.
// It panics if value is not an pointer to an interface.
func InterfaceOf(value interface{}) reflect.Type {
	t := reflect.TypeOf(value)

	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Interface {
		panic("called inject.InterfaceOf with a value that is not a pointer to an interface. (*MyInterface)(nil)")
	}

	return t
}

// New returns a new Injector.
func New() Injector {
	return &injector{
		values: make([]instance, 0),
	}
}

// Invoke attempts to call the interface{} provided as a function,
// providing dependencies for function arguments based on Type.
// Returns a slice of reflect.Value representing the returned values of the function.
// Returns an error if the injection fails.
// It panics if f is not a function
func (inj *injector) Invoke(f interface{}) ([]reflect.Value, error) {
	t := reflect.TypeOf(f)

	var in = make([]reflect.Value, t.NumIn()) //Panic if t is not kind of Func
	for i := 0; i < t.NumIn(); i++ {
		argType := t.In(i)
		val := inj.Get(argType)
		if !val.IsValid() {
			return nil, fmt.Errorf("value not found for type %v", argType)
		}

		in[i] = val
	}

	return reflect.ValueOf(f).Call(in), nil
}

// Maps dependencies in the Type map to each field in the struct
// that is tagged with 'inject'.
// Returns an error if the injection fails.
func (inj *injector) Apply(val interface{}) error {
	v := reflect.ValueOf(val)

	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil // Should not panic here ?
	}

	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		structField := t.Field(i)
		if f.CanSet() && (structField.Tag == "inject" || structField.Tag.Get("inject") != "") {
			ft := f.Type()
			v := inj.Get(ft)
			if !v.IsValid() {
				return fmt.Errorf("value not found for type %v", ft)
			}

			f.Set(v)
		}
	}

	return nil
}

// Maps the concrete value of val to its dynamic type using reflect.TypeOf,
// It returns the TypeMapper registered in.
func (i *injector) Map(val interface{}) TypeMapper {
	i.values = append(i.values, instance{reflect.TypeOf(val), reflect.ValueOf(val)})
	return i
}

func (i *injector) MapTo(val interface{}, ifacePtr interface{}) TypeMapper {
	i.values = append(i.values, instance{InterfaceOf(ifacePtr), reflect.ValueOf(val)})
	return i
}

// Provide the dynamic type of provider returns,
// It returns the TypeMapper registered in.
func (inj *injector) Provide(provider interface{}) TypeMapper {
	results, err := inj.Invoke(reflect.ValueOf(provider).Interface())
	if err != nil {
		panic(err)
	}

	for _, result := range results {
		resultType := result.Type()
		inj.values = append(inj.values, instance{resultType, result})
	}

	return inj
}

// Maps the given reflect.Type to the given reflect.Value and returns
// the Typemapper the mapping has been registered in.
// It panics if invoke provider failed.
func (i *injector) Set(typ reflect.Type, val reflect.Value) TypeMapper {
	i.values = append(i.values, instance{typ, val})
	return i
}

func (i *injector) Get(t reflect.Type) reflect.Value {
	for _, inst := range i.values {
		if inst.tp == t && inst.vl.IsValid() {
			return inst.vl
		}
	}

	// no concrete types found, try to find implementors
	// if t is an interface
	if t.Kind() == reflect.Interface {
		for _, inst := range i.values {
			if inst.tp.Implements(t) && inst.vl.IsValid() {
				return inst.vl
			}
		}
	}

	// Still no type found, try to look it up on the parent
	if i.parent != nil {
		return i.parent.Get(t)
	}

	panic(fmt.Sprint("no instance found for ", t))
}

func (i *injector) GetAll(t reflect.Type) []reflect.Value {
	var values []reflect.Value

	if t.Kind() != reflect.Interface {
		panic("cannot get all implementors for non interface type")
	}

	if t.Kind() == reflect.Interface {
		for _, inst := range i.values {
			if inst.tp.Implements(t) && inst.vl.IsValid() {
				values = append(values, inst.vl)
			}
		}
	}

	if i.parent != nil {
		parentVals := i.parent.GetAll(t)
		for i := range parentVals {
			values = append(values, parentVals[i])
		}
	}

	return values
}

func (i *injector) SetParent(parent Injector) {
	i.parent = parent
}
