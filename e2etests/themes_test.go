package e2etests

import (
	_ "embed"
	"testing"

	"github.com/d2lang/d2/d2themes/d2themescatalog"
)

func testThemes(t *testing.T) {
	tcs := []testCase{
		{
			name:    "dark terrastruct flagship",
			themeID: &d2themescatalog.DarkFlagshipTerrastruct.ID,
			script: `
network: {
  cell tower: {
		style.text-transform: capitalize
		satellites: {
			shape: stored_data
      style.multiple: true
		}

		transmitter : {
			style.text-transform: uppercase
		}

		satellites -> transmitter: SEnD {
			style.text-transform: lowercase
		}
		satellites -> transmitter: send
		satellites -> transmitter: send
  }

  online portal: {
    ui: { shape: hexagon }
  }

  data processor: {
    storage: {
      shape: cylinder
      style.multiple: true
    }
  }

  cell tower.transmitter -> data processor.storage: phone logs
}

user: {
  shape: person
  width: 130
}

user -> network.cell tower: make call
user -> network.online portal.ui: access {
  style.stroke-dash: 3
}

api server -> network.online portal.ui: display
api server -> logs: persist
logs: { shape: page; style.multiple: true }

network.data processor -> api server
users: {
	shape: sql_table
	id: int
	name: string
	email: string
	password: string
	last_login: datetime
}

products: {
	shape: class
	id: int
	price: decimal
	sku: string
	name: string
}
markdown: |md
  # A tale
  - of
  - two cities
|
code: |go
package main

import (
	"fmt"
)

type City struct {
	Name       string
	Population int
}

func tellTale(city1, city2 City) {
	fmt.Printf("There were two cities, %s and %s.\n", city1.Name, city2.Name)
	fmt.Printf("%s had a population of %d.\n", city1.Name, city1.Population)
	fmt.Printf("%s had a population of %d.\n", city2.Name, city2.Population)
	fmt.Println("Their tales were intertwined, and their people shared many adventures.")
}

func main() {
	city1 := City{Name: "CityA", Population: 1000000}
	city2 := City{Name: "CityB", Population: 1200000}

	tellTale(city1, city2)
}
|

markdown -> code -> ex
ex: |tex
	\displaylines{x = a + b \\ y = b + c}
	\sum_{k=1}^{n} h_{k} \int_{0}^{1} \bigl(\partial_{k} f(x_{k-1}+t h_{k} e_{k}) -\partial_{k} f(a)\bigr) \,dt
|
`,
		},
		{
			name:    "terminal",
			themeID: &d2themescatalog.Terminal.ID,
			script: `
network: {
  cell tower: {
		style.text-transform: capitalize
		satellites: {
			shape: stored_data
      style.multiple: true
		}

		transmitter : {
			style.text-transform: uppercase
		}

		satellites -> transmitter: SEnD {
			style.text-transform: lowercase
		}
		satellites -> transmitter: send
		satellites -> transmitter: send
  }

  online portal: {
    ui: { shape: hexagon }
  }

  data processor: {
    storage: {
      shape: cylinder
      style.multiple: true
    }
  }

  cell tower.transmitter -> data processor.storage: phone logs
}

user: {
  shape: person
  width: 130
}

user -> network.cell tower: make call
user -> network.online portal.ui: access {
  style.stroke-dash: 3
}

api server -> network.online portal.ui: display
api server -> logs: persist
logs: { shape: page; style.multiple: true }

network.data processor -> api server
users: {
	shape: sql_table
	id: int
	name: string
	email: string
	password: string
	last_login: datetime
}

products: {
	shape: class
	id: int
	price: decimal
	sku: string
	name: string
}
markdown: |md
  # A tale
  - of
  - two cities
|
code: |go
package main

import (
	"fmt"
)

type City struct {
	Name       string
	Population int
}

func tellTale(city1, city2 City) {
	fmt.Printf("There were two cities, %s and %s.\n", city1.Name, city2.Name)
	fmt.Printf("%s had a population of %d.\n", city1.Name, city1.Population)
	fmt.Printf("%s had a population of %d.\n", city2.Name, city2.Population)
	fmt.Println("Their tales were intertwined, and their people shared many adventures.")
}

func main() {
	city1 := City{Name: "CityA", Population: 1000000}
	city2 := City{Name: "CityB", Population: 1200000}

	tellTale(city1, city2)
}
|

markdown -> code -> ex
ex: |tex
	\displaylines{x = a + b \\ y = b + c}
	\sum_{k=1}^{n} h_{k} \int_{0}^{1} \bigl(\partial_{k} f(x_{k-1}+t h_{k} e_{k}) -\partial_{k} f(a)\bigr) \,dt
|
`,
		},
		{
			name:    "terminal_grayscale",
			themeID: &d2themescatalog.TerminalGrayscale.ID,
			script: `
network: {
  cell tower: {
		style.text-transform: capitalize
		satellites: {
			shape: stored_data
      style.multiple: true
		}

		transmitter

		satellites -> transmitter: SEnD {
			style.text-transform: lowercase
		}
		satellites -> transmitter: send
		satellites -> transmitter: send
  }

  online portal: {
    ui: { shape: hexagon }
  }

  data processor: {
    storage: {
      shape: cylinder
      style.multiple: true
    }
  }

  cell tower.transmitter -> data processor.storage: phone logs {
		style.text-transform: none
	}
}

user: {
  shape: person
  width: 130
}

user -> network.cell tower: make call
user -> network.online portal.ui: access {
  style.stroke-dash: 3
}

api server -> network.online portal.ui: display
api server -> logs: persist
logs: { shape: page; style.multiple: true }

network.data processor -> api server
`,
		},
		{
			name:    "origami",
			themeID: &d2themescatalog.Origami.ID,
			script: `
network: 通信網 {
  cell tower: {
		style.text-transform: capitalize
		satellites: 衛星 {
			shape: stored_data
      style.multiple: true
		}

		transmitter: 送信機

		satellites -> transmitter: SEnD {
			style.text-transform: lowercase
		}
		satellites -> transmitter: send {
			style.text-transform: uppercase
		}
		satellites -> transmitter: send
  }

  online portal: オンラインポータル {
    ui: { shape: hexagon }
  }

  data processor: データプロセッサ {
    storage: 保管所 {
      shape: cylinder
      style.multiple: true
    }
  }

  cell tower.transmitter -> data processor.storage: 電話ログ {
		style.text-transform: lowercase
	}
}

user: ユーザー {
  shape: person
  width: 130
	style.text-transform: capitalize
}

other-user: {
	shape: person
	style.text-transform: uppercase
}

user -> network.cell tower: 電話をかける
user -> network.online portal.ui: アクセス {
  style.stroke-dash: 3
}

api server: API サーバー {
	style.text-transform: lowercase
}
api server -> network.online portal.ui: 画面
api server -> logs: 持続する
logs: ログ { shape: page; style.multiple: true }

network.data processor -> api server
`,
		},
		{
			name:    "3d-sides",
			themeID: &d2themescatalog.Terminal.ID,
			script: `
beats: Beats {
  Explanation: Beats is a family of "data shippers," distinct services that send a single type of data from machines {
    grid-columns: 1
    style.stroke-width: 0
    Image: "" {
      icon: https://www.pngkey.com/png/full/75-752805_elastic-beats-logo-png-transparent-design.png
      shape: image
    }
  }

  style.3d: true
}
`,
		},
		{
			name:      "theme-color-codes-dark-mauve",
			themeID:   &d2themescatalog.DarkMauve.ID,
			justDagre: true,
			script:    themeColorCodesScript,
		},
		{
			name:      "theme-color-codes-terminal",
			themeID:   &d2themescatalog.Terminal.ID,
			justDagre: true,
			script:    themeColorCodesScript,
		},
	}

	runa(t, tcs)
}

// themeColorCodesScript draws every theme color code as a swatch, plus styled
// special shapes, so each theme shows what the codes resolve to.
const themeColorCodesScript = `
palette: Theme palette {
  grid-rows: 3
  grid-columns: 7
  grid-gap: 24
  N1: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: N1; stroke: N1}}
  N2: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: N2; stroke: N1}}
  N3: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: N3; stroke: N1}}
  N4: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: N4; stroke: N1}}
  N5: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: N5; stroke: N1}}
  N6: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: N6; stroke: N1}}
  N7: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: N7; stroke: N1}}
  B1: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: B1; stroke: N1}}
  B2: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: B2; stroke: N1}}
  B3: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: B3; stroke: N1}}
  B4: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: B4; stroke: N1}}
  B5: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: B5; stroke: N1}}
  B6: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: B6; stroke: N1}}
  gap60: "" {width: 64; height: 40; style.opacity: 0}
  AA2: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: AA2; stroke: N1}}
  AA4: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: AA4; stroke: N1}}
  AA5: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: AA5; stroke: N1}}
  AB4: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: AB4; stroke: N1}}
  AB5: {width: 64; height: 40; label.near: outside-bottom-center; style: {fill: AB5; stroke: N1}}
  gap50: "" {width: 64; height: 40; style.opacity: 0}
  gap51: "" {width: 64; height: 40; style.opacity: 0}
}

orders: {
  shape: sql_table
  id: int {constraint: primary_key}
  total: decimal
  style: {
    fill: AA2
    stroke: N6
    font-color: N7
  }
}
api: API {
  style: {
    fill: AB4
    stroke: AB5
    font-color: AA2
    3d: true
  }
}
api -> orders: writes {
  style: {
    stroke: AA4
    font-color: B2
    stroke-width: 3
  }
}
`
