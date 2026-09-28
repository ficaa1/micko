package shared

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/harmonica"
)

// On the floor, Mićko sits in the pane's bottom corner a hop or two from a
// little mirror, the way he sits on the floor next to the desk: he watches
// you, and now and then hops over to the mirror to kiss himself. The last row
// of each pose is drawn onto the pane's bottom border, which is the floor
// under his feet, his tail and the mirror's stand. The first row is empty
// air for him to hop into.
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

// floorHop is how far he hops from where he watches you to his mirror, in
// two hops. floorWidth is the width of every floor pose: that far, his
// drawing and the mirror.
const (
	floorHop   = 12
	floorWidth = floorHop + 12 + 3
)

// floorPose is one of Mićko's poses on the floor.
type floorPose struct {
	left  bool   // facing left, toward the pane and you, not the mirror
	eye   string // his eye, "o" or shut "-"
	lean  bool   // head leaned in: toward you or where he hops facing left, onto the glass facing right
	tail  string // the tail's four cells, when it flicks
	fluff bool   // feathers puffed up
	heart bool   // a heart over his beak, after a kiss
	tuck  bool   // feet tucked up, in the air
	x     int    // how far he is from where he watches you, toward the mirror
	lift  int    // how many rows he is off the floor, 0 or 1
}

// bird draws him in the pose, 4 rows of 12 columns.
func (p floorPose) bird() (lines, mask []string) {
	lines = append([]string(nil), mickoFloorBird.Lines...)
	mask = append([]string(nil), mickoFloorBird.Mask...)
	if p.eye != "" {
		lines[1] = strings.Replace(lines[1], "o", p.eye, 1)
	}
	if p.tail != "" {
		lines[3] = p.tail + lines[3][4:]
	}
	if p.fluff {
		lines[2] = strings.NewReplacer("(", "{", ")", "}").Replace(lines[2])
	}
	if p.tuck {
		// His feet, the scallops of his bottom row, fold into his belly.
		m := []byte(mask[3])
		for i := range lines[3] {
			if lines[3][i] == '^' {
				m[i] = 'r'
			}
		}
		lines[3], mask[3] = strings.ReplaceAll(lines[3], "^", "-"), string(m)
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
	return lines, mask
}

// art draws the pose on the floor: a row of air, then three rows and the
// floor, with the mirror standing at the right.
func (p floorPose) art() Art {
	rows := len(mickoFloorBird.Lines) + 1
	cells := make([][]rune, rows)
	parts := make([][]rune, rows)
	for i := range cells {
		cells[i] = []rune(strings.Repeat(" ", floorWidth))
		parts[i] = []rune(strings.Repeat(" ", floorWidth))
	}
	put := func(row, col int, line, mask string) {
		m := []rune(mask)
		for i, r := range []rune(line) {
			cells[row][col+i], parts[row][col+i] = r, m[i]
		}
	}
	lines, mask := p.bird()
	for i := range lines {
		put(1+i-p.lift, p.x, lines[i], mask[i])
	}
	for i := range mickoMirror.Lines {
		glass, glassMask := mickoMirror.Lines[i], mickoMirror.Mask[i]
		if i == 1 && p.lean && !p.left && p.x == floorHop {
			// His beak on the glass meets its reflection.
			glass, glassMask = "│<│", "gwg"
		}
		put(1+i, floorWidth-3, glass, glassMask)
	}
	a := Art{Lines: make([]string, rows), Mask: make([]string, rows)}
	for i := range cells {
		a.Lines[i], a.Mask[i] = string(cells[i]), string(parts[i])
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
// watching you, a hop or two from his mirror.
var MickoFloor = floorPose{left: true}.art()

// The spring his hops follow (see hops), and the rate they are drawn at.
const (
	hopFPS       = 60
	hopFrequency = 15.5
	hopDamping   = 0.9
)

// hops are the beats of Mićko hopping from x to each of stops in turn. For
// each hop he crouches, leaning the way he is going, and springs off with
// his feet tucked. His position follows a damped spring, drawn at hopFPS and
// rounded to the column, and the row he is off the floor follows an arc over
// how far along the hop he is, so he touches down where the spring lands him.
func hops(left bool, x int, stops ...int) []Beat {
	frame := time.Second / hopFPS
	spring := harmonica.NewSpring(harmonica.FPS(hopFPS), hopFrequency, hopDamping)
	var out []Beat
	for _, to := range stops {
		out = append(out, Beat{floorPose{left: left, lean: true, x: x}.art(), 110 * time.Millisecond})
		pos, vel, from := float64(x), 0.0, float64(x)
		air := true
		for n := 0; n < 3*hopFPS; n++ {
			pos, vel = spring.Update(pos, vel, float64(to))
			pos = math.Max(0, math.Min(floorHop, pos))
			gap := float64(to) - pos
			if air && (math.Abs(gap) < 0.5 || (gap > 0) != (float64(to) > from)) {
				air = false
			}
			if !air && math.Abs(gap) < 0.5 && math.Abs(vel) < 1.5 {
				break
			}
			pose := floorPose{left: left, x: int(math.Round(pos))}
			if air {
				along := (pos - from) / (float64(to) - from)
				pose.tuck, pose.lift = true, int(math.Round(math.Sin(math.Pi*math.Max(0, math.Min(1, along)))))
			}
			out = append(out, Beat{pose.art(), frame})
		}
		x = to
		out = append(out, Beat{floorPose{left: left, x: x}.art(), 120 * time.Millisecond})
	}
	return out
}

// merged joins each run of beats that draw the same pose into one beat, so
// a hop redraws him only when he moves a cell.
func merged(beats []Beat) []Beat {
	var out []Beat
	for _, b := range beats {
		if n := len(out); n > 0 && sameArt(out[n-1].Pose, b.Pose) {
			out[n-1].Hold += b.Hold
			continue
		}
		out = append(out, b)
	}
	return out
}

func sameArt(a, b Art) bool {
	return strings.Join(a.Lines, "\n") == strings.Join(b.Lines, "\n") && strings.Join(a.Mask, "\n") == strings.Join(b.Mask, "\n")
}

// MickoFloorBeats is Mićko's routine on the floor, played on a loop. He
// watches you, blinking, bobbing his head and flicking his tail, then turns
// and hops over to the mirror, kisses it twice, fluffs up and hops back. A
// pass takes about 50 seconds.
var MickoFloorBeats = func() []Beat {
	watch, blink := MickoFloor, floorPose{left: true, eye: "-"}.art()
	bob, flick := floorPose{left: true, lean: true}.art(), floorPose{left: true, tail: "~==="}.art()
	turn := floorPose{}.art()
	mirror, mirrorBlink := floorPose{x: floorHop}.art(), floorPose{x: floorHop, eye: "-"}.art()
	kiss, fluff := floorPose{x: floorHop, lean: true, heart: true}.art(), floorPose{x: floorHop, fluff: true}.art()
	turnBack := floorPose{left: true, x: floorHop}.art()
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	beats := []Beat{
		{watch, ms(5000)}, {blink, ms(150)},
		{watch, ms(6000)}, {bob, ms(400)}, {watch, ms(300)}, {bob, ms(400)},
		{watch, ms(5000)}, {flick, ms(250)}, {watch, ms(200)}, {flick, ms(250)},
		{watch, ms(6000)}, {blink, ms(150)},
		{watch, ms(8000)},
		{turn, ms(700)},
	}
	beats = append(beats, hops(false, 0, floorHop/2, floorHop)...)
	beats = append(beats,
		Beat{mirror, ms(1500)}, Beat{mirrorBlink, ms(150)}, Beat{mirror, ms(800)},
		Beat{kiss, ms(500)}, Beat{mirror, ms(400)}, Beat{kiss, ms(500)},
		Beat{mirror, ms(1200)}, Beat{fluff, ms(600)}, Beat{mirror, ms(1000)},
		Beat{turnBack, ms(600)},
	)
	beats = append(beats, hops(true, floorHop, floorHop/2, 0)...)
	return merged(beats)
}()
