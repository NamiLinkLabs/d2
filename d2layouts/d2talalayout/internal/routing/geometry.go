package routing

import (
	"github.com/d2lang/d2/lib/geo"
)

func segmentIntersectsBox(first, second *geo.Point, box *geo.Box) bool {
	if first == nil || second == nil || box == nil || box.TopLeft == nil {
		return false
	}
	left := min(box.TopLeft.X, box.TopLeft.X+box.Width)
	right := max(box.TopLeft.X, box.TopLeft.X+box.Width)
	top := min(box.TopLeft.Y, box.TopLeft.Y+box.Height)
	bottom := max(box.TopLeft.Y, box.TopLeft.Y+box.Height)
	if max(first.X, second.X) < left || right < min(first.X, second.X) ||
		max(first.Y, second.Y) < top || bottom < min(first.Y, second.Y) {
		return false
	}
	contains := func(point *geo.Point) bool {
		return left <= point.X && point.X <= right && top <= point.Y && point.Y <= bottom
	}
	if contains(first) || contains(second) {
		return true
	}
	tEnter, tExit := 0.0, 1.0
	clipAxis := func(start, delta, minCoord, maxCoord float64) bool {
		if delta == 0 {
			return minCoord <= start && start <= maxCoord
		}
		one := (minCoord - start) / delta
		two := (maxCoord - start) / delta
		if one > two {
			one, two = two, one
		}
		tEnter = max(tEnter, one)
		tExit = min(tExit, two)
		return tEnter <= tExit
	}
	if !clipAxis(first.X, second.X-first.X, left, right) ||
		!clipAxis(first.Y, second.Y-first.Y, top, bottom) {
		return false
	}
	return tEnter < tExit
}

func orientation(first, second, third *geo.Point) float64 {
	firstSecondX := second.X - first.X
	firstSecondY := second.Y - first.Y
	firstThirdX := third.X - first.X
	firstThirdY := third.Y - first.Y
	return firstSecondY*firstThirdX - firstSecondX*firstThirdY
}

func intersects(firstStart, firstEnd, secondStart, secondEnd *geo.Point) bool {
	secondStartSide := orientation(firstStart, firstEnd, secondStart)
	secondEndSide := orientation(firstStart, firstEnd, secondEnd)
	firstStartSide := orientation(secondStart, secondEnd, firstStart)
	firstEndSide := orientation(secondStart, secondEnd, firstEnd)
	if secondStartSide == 0 && secondEndSide == 0 && firstStartSide == 0 && firstEndSide == 0 {
		return closedIntervalsOverlap(firstStart.X, firstEnd.X, secondStart.X, secondEnd.X) &&
			closedIntervalsOverlap(firstStart.Y, firstEnd.Y, secondStart.Y, secondEnd.Y)
	}
	return straddlesLine(secondStartSide, secondEndSide) && straddlesLine(firstStartSide, firstEndSide)
}

func straddlesLine(first, second float64) bool {
	return first == 0 || second == 0 || (first < 0) != (second < 0)
}

func closedIntervalsOverlap(firstStart, firstEnd, secondStart, secondEnd float64) bool {
	firstMin, firstMax := min(firstStart, firstEnd), max(firstStart, firstEnd)
	secondMin, secondMax := min(secondStart, secondEnd), max(secondStart, secondEnd)
	return firstMin <= secondMax && secondMin <= firstMax
}
