package d2elklayout

import (
	"fmt"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

type elkPortKey struct {
	obj  *d2graph.Object
	side PortSide
}

func elkPortSide(side geo.Orientation) PortSide {
	switch side {
	case geo.Top:
		return North
	case geo.Bottom:
		return South
	case geo.Left:
		return West
	default:
		return East
	}
}

// growForPinnedPorts widens (or heightens) obj so that the connections pinned
// to one of its sides get port_spacing each, as ELK nodes already grow for
// their in- and out-degree.
func growForPinnedPorts(obj *d2graph.Object, counts map[elkPortKey]int) {
	horizontal := max(counts[elkPortKey{obj, North}], counts[elkPortKey{obj, South}])
	vertical := max(counts[elkPortKey{obj, West}], counts[elkPortKey{obj, East}])
	if horizontal >= 2 && obj.Attributes.WidthAttr == nil {
		obj.Width = max(obj.Width, float64(horizontal)*port_spacing)
	}
	if vertical >= 2 && obj.Attributes.HeightAttr == nil {
		obj.Height = max(obj.Height, float64(vertical)*port_spacing)
	}
}

// pinnedPortID names the ELK port of connection ei's pinned end on obj.
func pinnedPortID(obj *d2graph.Object, ei int, src bool) string {
	end := "dst"
	if src {
		end = "src"
	}
	return fmt.Sprintf("%s.__pin__.%d.%s", obj.AbsID(), ei, end)
}

// addPinnedPort adds a port on side to n for a connection end pinned with
// source-port / target-port and returns it. A node with pinned ports keeps
// each port on its side (FIXED_SIDE) but lets ELK order and place them;
// nodes that already fix port positions (sql_table) keep FIXED_POS, and
// spreadFixedPorts places the new port.
func addPinnedPort(n *ELKNode, id string, side PortSide) *ELKPort {
	if n.LayoutOptions == nil {
		n.LayoutOptions = &elkOpts{}
	}
	if n.LayoutOptions.PortConstraints == "" {
		n.LayoutOptions.PortConstraints = "FIXED_SIDE"
	}
	p := &ELKPort{
		ID:            id,
		LayoutOptions: &elkOpts{PortSide: side},
	}
	n.Ports = append(n.Ports, p)
	return p
}

// spreadFixedPorts spaces the ports collected for each side of a FIXED_POS
// node evenly along that side.
func spreadFixedPorts(elkNodes map[*d2graph.Object]*ELKNode, ports map[elkPortKey][]*ELKPort) {
	for k, ports := range ports {
		n := elkNodes[k.obj]
		switch k.side {
		case West, East:
			spacing := n.Height / float64(len(ports)+1)
			for i, p := range ports {
				p.Y = float64(i+1) * spacing
				if k.side == East {
					p.X = n.Width
				}
			}
		default:
			spacing := n.Width / float64(len(ports)+1)
			for i, p := range ports {
				p.X = float64(i+1) * spacing
			}
		}
	}
}
