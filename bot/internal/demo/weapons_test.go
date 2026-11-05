package demo

import (
	"slices"
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/common"
)

// The names of the grenades are the library's: Kill.Grenades and Hit.Weapon
// take them from it.
func TestGrenadeNamesAndPrices(t *testing.T) {
	for _, tc := range []struct {
		name  string
		eq    common.EquipmentType
		price int
	}{
		{Flashbang, common.EqFlash, 200},
		{HEGrenade, common.EqHE, 300},
		{SmokeGrenade, common.EqSmoke, 300},
		{Molotov, common.EqMolotov, 400},
		{Incendiary, common.EqIncendiary, 500},
		{Decoy, common.EqDecoy, 50},
	} {
		if got := tc.eq.String(); got != tc.name {
			t.Errorf("the library names %v %q, we %q", tc.eq, got, tc.name)
		}
		if got := GrenadePrice(tc.name); got != tc.price {
			t.Errorf("price of %s = %d, want %d", tc.name, got, tc.price)
		}
	}
	if got := GrenadePrice("AK-47"); got != 0 {
		t.Errorf("price of a rifle as a grenade = %d, want 0", got)
	}
}

func TestMaxSpeed(t *testing.T) {
	for _, tc := range []struct {
		eq     common.EquipmentType
		zoomed bool
		want   float64
	}{
		{common.EqAK47, false, 215},
		{common.EqAK47, true, 215},
		{common.EqAWP, false, 200},
		{common.EqAWP, true, 100},
		{common.EqAUG, true, 150},
		{common.EqDeagle, false, 230},
		{common.EqKnife, false, 0}, // not a gun
		{common.EqZeus, false, 0},
	} {
		if got := maxSpeed(tc.eq, tc.zoomed); got != tc.want {
			t.Errorf("maxSpeed(%v, zoomed %t) = %v, want %v", tc.eq, tc.zoomed, got, tc.want)
		}
	}
	// Every gun the library knows has a speed: the rest is grenades and
	// gear. A number the library has no weapon for has no name.
	for eq := common.EquipmentType(1); eq < 600; eq++ {
		if name := eq.String(); name == "" || name == "UNKNOWN" {
			continue
		}
		if c := weaponClass(common.NewEquipment(eq, 0)); gun(c) && maxSpeed(eq, false) == 0 {
			t.Errorf("gun %v (%s) has no top speed", eq, c)
		}
	}
}

func TestGrenadesCarried(t *testing.T) {
	inv := func(eqs ...common.EquipmentType) *common.Player {
		p := &common.Player{Inventory: make(map[int]*common.Equipment)}
		for i, eq := range eqs {
			p.Inventory[i+1] = common.NewEquipment(eq, common.UniqueID(i+1))
		}
		return p
	}
	for _, tc := range []struct {
		p    *common.Player
		want []string
	}{
		{nil, nil},
		{inv(common.EqAK47, common.EqKnife), nil},
		{inv(common.EqHE, common.EqAK47, common.EqSmoke, common.EqIncendiary), []string{HEGrenade, SmokeGrenade, Incendiary}},
		// Without a pawn the library counts no flashbang ammo: one each.
		{inv(common.EqFlash, common.EqMolotov), []string{Molotov, Flashbang}},
		{inv(common.EqFlash, common.EqFlash, common.EqDecoy), []string{Decoy, Flashbang, Flashbang}},
	} {
		if got := grenades(tc.p); !slices.Equal(got, tc.want) {
			t.Errorf("grenades = %q, want %q", got, tc.want)
		}
	}
}

func TestGun(t *testing.T) {
	for c, want := range map[WeaponClass]bool{
		ClassPistol: true, ClassSMG: true, ClassShotgun: true, ClassRifle: true, ClassSniper: true, ClassMachineGun: true,
		ClassKnife: false, ClassZeus: false, ClassGrenade: false, ClassFire: false, ClassBomb: false, ClassWorld: false, ClassOther: false,
	} {
		if got := gun(c); got != want {
			t.Errorf("gun(%s) = %t, want %t", c, got, want)
		}
	}
}
