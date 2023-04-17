package go2ts

import (
	"image/color"
	"log"
	"os"
)

func Example() {
	type Direction string

	const (
		Up    Direction = "up"
		Down  Direction = "down"
		Left  Direction = "left"
		Right Direction = "right"
	)

	AllDirections := []Direction{Up, Down, Left, Right}

	type Position struct {
		X int
		Y int
	}

	type Turtle struct {
		Position  Position `json:"Coordinates"`
		Color     color.Alpha
		Direction Direction
	}

	generator := New()
	generator.Add(Turtle{})
	generator.AddUnion(AllDirections)
	err := generator.Render(os.Stdout)
	if err != nil {
		log.Fatal(err)
	}

	// Output:
	// // DO NOT EDIT. This file is automatically generated.
	//
	// export interface Position {
	// 	X: number;
	// 	Y: number;
	// }
	//
	// export interface Alpha {
	// 	A: number;
	// }
	//
	// export interface Turtle {
	// 	Coordinates: Position;
	// 	Color: Alpha;
	// 	Direction: Direction;
	// }
	//
	// export type Direction = 'up' | 'down' | 'left' | 'right';
	//
}
