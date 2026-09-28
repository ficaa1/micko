package shared

import (
	"strings"
	"time"
)

// On the floor, Mićko sits in the pane's bottom corner beside a little
// mirror, the way he sits on the floor next to the desk: he watches you, and
// now and then turns to the mirror to kiss himself. The last row of each
// pose is drawn onto the pane's bottom border, which is the floor under his
// feet, his tail and the mirror's stand.
//
// The mask adds two parts to the perch's: g is the mirror, h the heart he
// sends it.

// mickoFloorBird is Mićko on the floor facing right, toward the mirror, 12
// columns wide. The other poses are made from it.
var mickoFloorBird = Art{
	Lines: []string{
		"     .--.   ",
		"    (  o )> ",
		"    (^v^v^) ",
		"====`-^-^-' ",
	},
	Mask: []string{
		"     rrrr   ",
		"    r  k rw ",
		"    rbkbkbr ",
		"bbbbrrkrkrr ",
	},
}

// mickoMirror stands at the right of every floor pose.
var mickoMirror = Art{
	Lines: []string{"╭─╮", "│░│", "╰┬╯", " ┴ "},
	Mask:  []string{"ggg", "ggg", "ggg", " g "},
}

// floorPose is one of Mićko's poses on the floor.
type floorPose struct {
	left  bool   // facing left, toward the pane and you, not the mirror
	eye   string // his eye, "o" or shut "-"
	lean  bool   // head leaned in: toward you facing left, onto the glass facing right
	tail  string // the tail's four cells, when it flicks
	fluff bool   // feathers puffed up
	heart bool   // a heart over his beak, after a kiss
}

// art draws the pose beside the mirror.
func (p floorPose) art() Art {
	lines := append([]string(nil), mickoFloorBird.Lines...)
	mask := append([]string(nil), mickoFloorBird.Mask...)
	if p.eye != "" {
		lines[1] = strings.Replace(lines[1], "o", p.eye, 1)
	}
	if p.tail != "" {
		lines[3] = p.tail + lines[3][4:]
	}
	if p.fluff {
		lines[2] = strings.NewReplacer("(", "{", ")", "}").Replace(lines[2])
	}
	if p.left {
		for i := range lines {
			lines[i], mask[i] = mirrored(lines[i]), reversed(mask[i])
		}
	}
	if p.lean {
		// Only his head moves: its rows shift a cell toward where he looks.
		for _, row := range []int{0, 1} {
			if p.left {
				lines[row], mask[row] = lines[row][1:]+" ", mask[row][1:]+" "
			} else {
				lines[row], mask[row] = " "+lines[row][:len(lines[row])-1], " "+mask[row][:len(mask[row])-1]
			}
		}
	}
	if p.heart {
		lines[0], mask[0] = lines[0][:11]+"♥", mask[0][:11]+"h"
	}
	a := Art{Lines: make([]string, len(lines)), Mask: make([]string, len(lines))}
	for i := range lines {
		glass, glassMask := mickoMirror.Lines[i], mickoMirror.Mask[i]
		if i == 1 && p.lean && !p.left {
			// His beak on the glass meets its reflection.
			glass, glassMask = "│<│", "gwg"
		}
		a.Lines[i], a.Mask[i] = lines[i]+glass, mask[i]+glassMask
	}
	return a
}

// mirrored is s seen in a mirror: reversed, with each character that has a
// direction turned round.
func mirrored(s string) string {
	flip := map[rune]rune{'(': ')', ')': '(', '<': '>', '>': '<', '`': '\'', '\'': '`', '{': '}', '}': '{', '/': '\\', '\\': '/'}
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	for i, c := range r {
		if f, ok := flip[c]; ok {
			r[i] = f
		}
	}
	return string(r)
}

// reversed is s back to front.
func reversed(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// MickoFloor is Mićko on the floor in his resting pose: facing the pane,
// watching you, with his mirror behind him.
var MickoFloor = floorPose{left: true}.art()

// MickoFloorBeats is Mićko's routine on the floor, played on a loop. He
// watches you, blinking, bobbing his head and flicking his tail, then turns
// to the mirror, kisses it twice, fluffs up and turns back. A pass takes
// about 45 seconds.
var MickoFloorBeats = func() []Beat {
	watch, blink := MickoFloor, floorPose{left: true, eye: "-"}.art()
	bob, flick := floorPose{left: true, lean: true}.art(), floorPose{left: true, tail: "~==="}.art()
	mirror, mirrorBlink := floorPose{}.art(), floorPose{eye: "-"}.art()
	kiss, fluff := floorPose{lean: true, heart: true}.art(), floorPose{fluff: true}.art()
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	return []Beat{
		{watch, ms(5000)}, {blink, ms(150)},
		{watch, ms(6000)}, {bob, ms(400)}, {watch, ms(300)}, {bob, ms(400)},
		{watch, ms(5000)}, {flick, ms(250)}, {watch, ms(200)}, {flick, ms(250)},
		{watch, ms(6000)}, {blink, ms(150)},
		{watch, ms(8000)},
		{mirror, ms(1500)}, {mirrorBlink, ms(150)}, {mirror, ms(800)},
		{kiss, ms(500)}, {mirror, ms(400)}, {kiss, ms(500)},
		{mirror, ms(1200)}, {fluff, ms(600)}, {mirror, ms(1000)},
	}
}()
