package parse

import "fmt"

// fitSportNames and fitSubSportNames are the FIT SDK's "sport" and "sub_sport" enum types
// (global message 18 "session", fields 5 and 6) — transcribed programmatically, not by
// hand, from python-fitparse's profile.py (github.com/dtcooper/python-fitparse), which is
// itself auto-generated from Garmin's own FIT SDK Profile.xlsx. Hand-typing ~110 numeric
// enum mappings that can't be verified against a live device is exactly the kind of thing
// that goes silently wrong; generating them from a verified source instead removes that
// risk. 254 ("all") is a real SDK value (a goals-only sentinel, per its own comment) kept
// here for completeness even though it's very unlikely to appear in a recorded session.
var fitSportNames = map[byte]string{
	0:   "generic",
	1:   "running",
	2:   "cycling",
	3:   "transition",
	4:   "fitness_equipment",
	5:   "swimming",
	6:   "basketball",
	7:   "soccer",
	8:   "tennis",
	9:   "american_football",
	10:  "training",
	11:  "walking",
	12:  "cross_country_skiing",
	13:  "alpine_skiing",
	14:  "snowboarding",
	15:  "rowing",
	16:  "mountaineering",
	17:  "hiking",
	18:  "multisport",
	19:  "paddling",
	20:  "flying",
	21:  "e_biking",
	22:  "motorcycling",
	23:  "boating",
	24:  "driving",
	25:  "golf",
	26:  "hang_gliding",
	27:  "horseback_riding",
	28:  "hunting",
	29:  "fishing",
	30:  "inline_skating",
	31:  "rock_climbing",
	32:  "sailing",
	33:  "ice_skating",
	34:  "sky_diving",
	35:  "snowshoeing",
	36:  "snowmobiling",
	37:  "stand_up_paddleboarding",
	38:  "surfing",
	39:  "wakeboarding",
	40:  "water_skiing",
	41:  "kayaking",
	42:  "rafting",
	43:  "windsurfing",
	44:  "kitesurfing",
	45:  "tactical",
	46:  "jumpmaster",
	47:  "boxing",
	48:  "floor_climbing",
	254: "all",
}

var fitSubSportNames = map[byte]string{
	0:   "generic",
	1:   "treadmill",
	2:   "street",
	3:   "trail",
	4:   "track",
	5:   "spin",
	6:   "indoor_cycling",
	7:   "road",
	8:   "mountain",
	9:   "downhill",
	10:  "recumbent",
	11:  "cyclocross",
	12:  "hand_cycling",
	13:  "track_cycling",
	14:  "indoor_rowing",
	15:  "elliptical",
	16:  "stair_climbing",
	17:  "lap_swimming",
	18:  "open_water",
	19:  "flexibility_training",
	20:  "strength_training",
	21:  "warm_up",
	22:  "match",
	23:  "exercise",
	24:  "challenge",
	25:  "indoor_skiing",
	26:  "cardio_training",
	27:  "indoor_walking",
	28:  "e_bike_fitness",
	29:  "bmx",
	30:  "casual_walking",
	31:  "speed_walking",
	32:  "bike_to_run_transition",
	33:  "run_to_bike_transition",
	34:  "swim_to_bike_transition",
	35:  "atv",
	36:  "motocross",
	37:  "backcountry",
	38:  "resort",
	39:  "rc_drone",
	40:  "wingsuit",
	41:  "whitewater",
	42:  "skate_skiing",
	43:  "yoga",
	44:  "pilates",
	45:  "indoor_running",
	46:  "gravel_cycling",
	47:  "e_bike_mountain",
	48:  "commuting",
	49:  "mixed_surface",
	50:  "navigate",
	51:  "track_me",
	52:  "map",
	53:  "single_gas_diving",
	54:  "multi_gas_diving",
	55:  "gauge_diving",
	56:  "apnea_diving",
	57:  "apnea_hunting",
	58:  "virtual_activity",
	59:  "obstacle",
	254: "all",
}

// sportName resolves a session message's sport/sub_sport enum pair to the single string
// activity_type stores. sub_sport wins whenever it says something sport alone doesn't —
// "gravel_cycling" over "cycling" — since that's the whole reason this gap mattered
// (IMPLEMENTATION.md §4.7). 0 ("generic") and 254 ("all") aren't real
// distinctions, so those fall back to sport. 0xFF is the FIT invalid-value sentinel for a
// one-byte field; an unset or missing sport falls back to "unknown", matching what GPX and
// TCX already default to when their own source data doesn't say either.
func sportName(sport, subSport byte) string {
	if subSport != 0 && subSport != 254 {
		if name, ok := fitSubSportNames[subSport]; ok {
			return name
		}
	}
	if sport == 0xFF {
		return "unknown"
	}
	if name, ok := fitSportNames[sport]; ok {
		return name
	}
	// A real but unmapped value — this table is complete as of the fetched SDK profile,
	// so reaching here means either a newer SDK revision or a nonconformant encoder, not a
	// bug in the table. Honest fallback over silently discarding the value.
	return fmt.Sprintf("sport_%d", sport)
}
