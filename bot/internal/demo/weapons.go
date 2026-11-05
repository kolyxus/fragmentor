package demo

import "github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs/common"

// Weapon data: the kind of each weapon, the top speed of a player with it
// and the prices of the grenades.
//
// The speeds and the prices are CS2's own: m_flMaxSpeed and m_nPrice in the
// game's scripts/weapons.vdata, as SteamDB's GameTracking-CS2 mirrors it,
// https://github.com/SteamDatabase/GameTracking-CS2/blob/master/game/csgo/pak01_dir/scripts/weapons.vdata,
// taken on 2026-11-05. demoinfocs has neither. A big patch of CS2 may change
// them: then check them again, see docs/METRICS.md.

// The grenades by the names Kill.Grenades, Hit.Weapon and Shot.Weapon give
// them, which are demoinfocs' names of the weapons.
const (
	Flashbang    = "Flashbang"
	HEGrenade    = "HE Grenade"
	SmokeGrenade = "Smoke Grenade"
	Molotov      = "Molotov"
	Incendiary   = "Incendiary Grenade"
	Decoy        = "Decoy Grenade"
)

// grenadePrices is m_nPrice of each grenade, in dollars of the game.
var grenadePrices = map[string]int{
	Flashbang:    200,
	HEGrenade:    300,
	SmokeGrenade: 300,
	Molotov:      400,
	Incendiary:   500,
	Decoy:        50,
}

// GrenadePrice returns the price of the grenade named name in dollars of
// the game, 0 if name is not a grenade.
func GrenadePrice(name string) int { return grenadePrices[name] }

// maxSpeeds is m_flMaxSpeed of each gun, units per second: the top speed of
// a player with the gun in hand, and scoped in, which slows a player down
// with a sniper rifle, the AUG and the SG 553. Movement makes a gun
// inaccurate above 34% of the speed, see docs/METRICS.md.
var maxSpeeds = map[common.EquipmentType][2]float64{
	common.EqGlock:        {240, 240},
	common.EqUSP:          {240, 240},
	common.EqP2000:        {240, 240},
	common.EqP250:         {240, 240},
	common.EqFiveSeven:    {240, 240},
	common.EqTec9:         {240, 240},
	common.EqCZ:           {240, 240},
	common.EqDualBerettas: {240, 240},
	common.EqDeagle:       {230, 230},
	common.EqRevolver:     {180, 220},
	common.EqMac10:        {240, 240},
	common.EqMP9:          {240, 240},
	common.EqMP7:          {220, 220},
	common.EqMP5:          {235, 220},
	common.EqUMP:          {230, 230},
	common.EqP90:          {230, 230},
	common.EqBizon:        {240, 240},
	common.EqNova:         {220, 220},
	common.EqXM1014:       {215, 215},
	common.EqSwag7:        {225, 225},
	common.EqSawedOff:     {210, 210},
	common.EqM249:         {195, 195},
	common.EqNegev:        {150, 150},
	common.EqGalil:        {215, 215},
	common.EqFamas:        {220, 220},
	common.EqAK47:         {215, 215},
	common.EqM4A4:         {225, 225},
	common.EqM4A1:         {225, 225},
	common.EqSG553:        {210, 150},
	common.EqAUG:          {220, 150},
	common.EqSSG08:        {230, 230},
	common.EqAWP:          {200, 100},
	common.EqScar20:       {215, 120},
	common.EqG3SG1:        {215, 120},
}

// maxSpeed returns the top speed with the gun t, scoped in if zoomed, or 0
// if the gun is unknown.
func maxSpeed(t common.EquipmentType, zoomed bool) float64 {
	s, ok := maxSpeeds[t]
	switch {
	case !ok:
		return 0
	case zoomed:
		return s[1]
	}
	return s[0]
}

func weaponName(w *common.Equipment) string {
	if w == nil {
		return ""
	}
	return w.Type.String()
}

// weaponClass tells the kind of the weapon. The library's classes put
// shotguns and machine guns together, snipers among rifles, the knife and
// the zeus among the equipment and fire among the grenades.
func weaponClass(w *common.Equipment) WeaponClass {
	if w == nil {
		return ClassOther
	}
	switch w.Type {
	case common.EqSawedOff, common.EqNova, common.EqSwag7, common.EqXM1014:
		return ClassShotgun
	case common.EqM249, common.EqNegev:
		return ClassMachineGun
	case common.EqSSG08, common.EqAWP, common.EqScar20, common.EqG3SG1:
		return ClassSniper
	case common.EqKnife, common.EqFists, common.EqAxe, common.EqHammer, common.EqWrench:
		return ClassKnife
	case common.EqZeus:
		return ClassZeus
	case common.EqMolotov, common.EqIncendiary:
		return ClassFire
	case common.EqBomb:
		return ClassBomb
	case common.EqWorld:
		return ClassWorld
	}
	switch w.Type.Class() {
	case common.EqClassPistols:
		return ClassPistol
	case common.EqClassSMG:
		return ClassSMG
	case common.EqClassRifle:
		return ClassRifle
	case common.EqClassGrenade:
		return ClassGrenade
	}
	return ClassOther
}

// gun reports whether the class is of a gun: a pistol, an SMG, a shotgun, a
// rifle, a sniper rifle or a machine gun.
func gun(c WeaponClass) bool {
	switch c {
	case ClassPistol, ClassSMG, ClassShotgun, ClassRifle, ClassSniper, ClassMachineGun:
		return true
	}
	return false
}

// grenades returns the grenades p carries, one name for each: two
// flashbangs are two. CS2 keeps both flashbangs in one weapon and counts
// them in the ammo of the pawn.
func grenades(p *common.Player) []string {
	if p == nil {
		return nil
	}
	var out []string
	flashbangs := 0
	for _, w := range p.Weapons() {
		switch {
		case w == nil || w.Class() != common.EqClassGrenade:
		case w.Type == common.EqFlash:
			flashbangs++
		default:
			out = append(out, weaponName(w))
		}
	}
	if flashbangs > 0 {
		flashbangs = max(flashbangs, int(p.FlashbangCount()))
	}
	for range flashbangs {
		out = append(out, Flashbang)
	}
	return out
}
