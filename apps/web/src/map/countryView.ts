import type { ViewState } from './viewState';

/**
 * Per-country opening view for MapView's zero-history fallback (docs/SPEC.md FR-4.5) — a
 * point + zoom that keeps roughly the whole country in frame, not a real-time lookup.
 *
 * Generated once, offline, from the same Admin-0 country polygons already seeded server-side
 * for Fog/Heatmap's country unlocking (`admin_countries`, IMPLEMENTATION.md §4.4/§4.4.x):
 * `ST_PointOnSurface(geom)` per `iso_a2` (not `ST_Centroid`, which can land outside a concave
 * or archipelago shape), zoom derived from the polygon's own bounding-box extent so a small
 * country lands close and a large one lands wide. Scoped to exactly the codes
 * the Settings page's Country list (`services/server/internal/web/places_data.go`) can write into `users.country` — a handful of
 * small territories in that list have no polygon in `admin_countries` and simply have no entry
 * here, falling through to WORLD_VIEW (config.ts) like an unset country does.
 *
 * A few entries are hand-corrected rather than derived: countries whose `admin_countries`
 * polygon bundles a far-flung overseas dependency into the same geometry as the mainland
 * (France, Netherlands — a correct mainland centroid but a wildly oversized bbox; Norway — a
 * centroid pulled all the way to Svalbard) would otherwise produce a broken or absurdly
 * zoomed-out view; and every antimeridian-crossing country (Russia, the United States, Fiji,
 * Kiribati, New Zealand, Antarctica) has a bbox-width/height calculation that breaks outright
 * at the ±180° seam.
 */
export const COUNTRY_VIEWS: Record<string, ViewState> = {
  AD: { longitude: 1.5709, latitude: 42.537, zoom: 8.0 }, // Andorra
  AE: { longitude: 55.1254, latitude: 24.3441, zoom: 7.0 }, // United Arab Emirates
  AF: { longitude: 65.1935, latitude: 33.9241, zoom: 5.4 }, // Afghanistan
  AG: { longitude: -61.7857, latitude: 17.084, zoom: 8.0 }, // Antigua & Barbuda
  AI: { longitude: -63.0844, latitude: 18.211, zoom: 8.0 }, // Anguilla
  AL: { longitude: 20.0042, latitude: 41.1821, zoom: 7.7 }, // Albania
  AM: { longitude: 44.8366, latitude: 40.0875, zoom: 7.6 }, // Armenia
  AO: { longitude: 18.8634, latitude: -11.9003, zoom: 5.5 }, // Angola
  AQ: { longitude: 0.0, latitude: -82.0, zoom: 2.0 }, // Antarctica
  AR: { longitude: -63.9713, latitude: -37.0856, zoom: 4.2 }, // Argentina
  AS: { longitude: -170.7192, latitude: -14.2972, zoom: 8.0 }, // American Samoa
  AT: { longitude: 14.7521, latitude: 47.7007, zoom: 6.3 }, // Austria
  AU: { longitude: 133.0765, latitude: -24.934, zoom: 3.7 }, // Australia
  AW: { longitude: -69.9887, latitude: 12.5237, zoom: 8.0 }, // Aruba
  AX: { longitude: 20.052, latitude: 60.2471, zoom: 8.0 }, // Åland Islands
  AZ: { longitude: 47.7315, latitude: 40.1397, zoom: 6.8 }, // Azerbaijan
  BA: { longitude: 17.9221, latitude: 43.9282, zoom: 7.3 }, // Bosnia & Herzegovina
  BB: { longitude: -59.5506, latitude: 13.1748, zoom: 8.0 }, // Barbados
  BD: { longitude: 89.8682, latitude: 23.6807, zoom: 6.7 }, // Bangladesh
  BE: { longitude: 4.7976, latitude: 50.5034, zoom: 7.3 }, // Belgium
  BF: { longitude: -1.2025, latitude: 12.2522, zoom: 6.3 }, // Burkina Faso
  BG: { longitude: 25.1591, latitude: 42.7332, zoom: 6.6 }, // Bulgaria
  BH: { longitude: 50.549, latitude: 26.0304, zoom: 8.0 }, // Bahrain
  BI: { longitude: 29.9144, latitude: -3.3775, zoom: 8.0 }, // Burundi
  BJ: { longitude: 2.2792, latitude: 9.3028, zoom: 6.6 }, // Benin
  BL: { longitude: -62.8376, latitude: 17.8971, zoom: 8.0 }, // St. Barthélemy
  BM: { longitude: -64.7148, latitude: 32.3448, zoom: 8.0 }, // Bermuda
  BN: { longitude: 114.491, latitude: 4.5024, zoom: 8.0 }, // Brunei
  BO: { longitude: -64.15, latitude: -16.2961, zoom: 5.5 }, // Bolivia
  BR: { longitude: -49.723, latitude: -14.2239, zoom: 4.0 }, // Brazil
  BS: { longitude: -78.3762, latitude: 26.6483, zoom: 6.6 }, // Bahamas
  BT: { longitude: 90.327, latitude: 27.5057, zoom: 7.5 }, // Bhutan
  BW: { longitude: 24.4787, latitude: -22.3368, zoom: 6.0 }, // Botswana
  BY: { longitude: 27.939, latitude: 53.7371, zoom: 6.0 }, // Belarus
  BZ: { longitude: -88.7343, latitude: 17.0776, zoom: 7.9 }, // Belize
  CA: { longitude: -110.4261, latitude: 56.8256, zoom: 2.8 }, // Canada
  CD: { longitude: 22.4367, latitude: -4.0595, zoom: 5.0 }, // Congo - Kinshasa
  CF: { longitude: 20.6187, latitude: 6.5955, zoom: 5.6 }, // Central African Republic
  CG: { longitude: 16.0404, latitude: -0.6967, zoom: 6.1 }, // Congo - Brazzaville
  CH: { longitude: 8.4234, latitude: 46.7939, zoom: 7.1 }, // Switzerland
  CI: { longitude: -5.6951, latitude: 7.5317, zoom: 6.6 }, // Côte d'Ivoire
  CK: { longitude: -159.7886, latitude: -21.2186, zoom: 8.0 }, // Cook Islands
  CL: { longitude: -71.5023, latitude: -35.6844, zoom: 3.8 }, // Chile
  CM: { longitude: 13.6141, latitude: 7.3794, zoom: 5.7 }, // Cameroon
  CN: { longitude: 98.5822, latitude: 36.8986, zoom: 3.3 }, // China
  CO: { longitude: -72.587, latitude: 4.1087, zoom: 5.2 }, // Colombia
  CR: { longitude: -83.6552, latitude: 9.6334, zoom: 7.5 }, // Costa Rica
  CU: { longitude: -77.9579, latitude: 21.5267, zoom: 5.8 }, // Cuba
  CV: { longitude: -25.1738, latitude: 17.0585, zoom: 7.8 }, // Cape Verde
  CW: { longitude: -68.9846, latitude: 12.1949, zoom: 8.0 }, // Curaçao
  CY: { longitude: 32.9776, latitude: 34.8799, zoom: 8.0 }, // Cyprus
  CZ: { longitude: 15.5146, latitude: 49.809, zoom: 6.5 }, // Czechia
  DE: { longitude: 10.4752, latitude: 51.0759, zoom: 6.1 }, // Germany
  DJ: { longitude: 42.3998, latitude: 11.7842, zoom: 8.0 }, // Djibouti
  DK: { longitude: 9.2638, latitude: 56.2639, zoom: 6.4 }, // Denmark
  DM: { longitude: -61.3574, latitude: 15.4625, zoom: 8.0 }, // Dominica
  DO: { longitude: -70.124, latitude: 18.7681, zoom: 7.4 }, // Dominican Republic
  DZ: { longitude: 0.6019, latitude: 27.9145, zoom: 4.9 }, // Algeria
  EC: { longitude: -78.2717, latitude: -1.7277, zoom: 5.2 }, // Ecuador
  EE: { longitude: 25.5018, latitude: 58.6172, zoom: 6.6 }, // Estonia
  EG: { longitude: 29.4532, latitude: 26.8492, zoom: 5.7 }, // Egypt
  EH: { longitude: -12.5962, latitude: 24.1954, zoom: 6.2 }, // Western Sahara
  ER: { longitude: 38.1052, latitude: 15.1766, zoom: 6.5 }, // Eritrea
  ES: { longitude: -3.4871, latitude: 39.9065, zoom: 4.8 }, // Spain
  ET: { longitude: 38.9442, latitude: 9.1731, zoom: 5.4 }, // Ethiopia
  FI: { longitude: 27.4395, latitude: 64.9337, zoom: 5.8 }, // Finland
  FJ: { longitude: 178.5, latitude: -17.8, zoom: 6.3 }, // Fiji
  FK: { longitude: -58.7699, latitude: -51.8015, zoom: 7.5 }, // Falkland Islands
  FM: { longitude: 158.2422, latitude: 6.888, zoom: 4.6 }, // Micronesia
  FO: { longitude: -7.2349, latitude: 62.0877, zoom: 8.0 }, // Faroe Islands
  FR: { longitude: 2.2, latitude: 46.7, zoom: 5.7 }, // France
  GA: { longitude: 11.6209, latitude: -0.811, zoom: 6.6 }, // Gabon
  GB: { longitude: -1.9576, latitude: 54.3504, zoom: 5.8 }, // United Kingdom
  GD: { longitude: -61.6743, latitude: 12.1468, zoom: 8.0 }, // Grenada
  GE: { longitude: 43.6369, latitude: 42.296, zoom: 6.5 }, // Georgia
  GG: { longitude: -2.5867, latitude: 49.4596, zoom: 8.0 }, // Guernsey
  GH: { longitude: -1.0685, latitude: 7.9771, zoom: 6.6 }, // Ghana
  GL: { longitude: -37.6363, latitude: 71.7008, zoom: 3.3 }, // Greenland
  GM: { longitude: -14.3593, latitude: 13.4418, zoom: 7.7 }, // Gambia
  GN: { longitude: -9.6725, latitude: 9.9492, zoom: 6.4 }, // Guinea
  GQ: { longitude: 10.5073, latitude: 1.7031, zoom: 7.7 }, // Equatorial Guinea
  GR: { longitude: 21.8069, latitude: 39.0845, zoom: 6.2 }, // Greece
  GS: { longitude: -36.461, latitude: -54.4313, zoom: 5.7 }, // South Georgia & South Sandwich Islands
  GT: { longitude: -90.2819, latitude: 15.7854, zoom: 7.2 }, // Guatemala
  GU: { longitude: 144.7835, latitude: 13.4778, zoom: 8.0 }, // Guam
  GW: { longitude: -14.6089, latitude: 11.8025, zoom: 7.7 }, // Guinea-Bissau
  GY: { longitude: -58.9607, latitude: 4.8505, zoom: 6.4 }, // Guyana
  HK: { longitude: 114.116, latitude: 22.4328, zoom: 8.0 }, // Hong Kong SAR China
  HM: { longitude: 73.5091, latitude: -53.0605, zoom: 8.0 }, // Heard & McDonald Islands
  HN: { longitude: -87.2443, latitude: 14.4929, zoom: 6.6 }, // Honduras
  HR: { longitude: 15.3399, latitude: 44.7362, zoom: 6.7 }, // Croatia
  HT: { longitude: -72.25, latitude: 18.9535, zoom: 7.8 }, // Haiti
  HU: { longitude: 19.1183, latitude: 47.1847, zoom: 6.5 }, // Hungary
  ID: { longitude: 113.3519, latitude: 0.0957, zoom: 3.7 }, // Indonesia
  IE: { longitude: -8.1135, latitude: 53.4292, zoom: 7.1 }, // Ireland
  IL: { longitude: 34.67, latitude: 31.4511, zoom: 7.3 }, // Israel
  IM: { longitude: -4.5114, latitude: 54.2462, zoom: 8.0 }, // Isle of Man
  IN: { longitude: 80.2313, latitude: 21.7839, zoom: 4.4 }, // India
  IO: { longitude: 72.4148, latitude: -7.3221, zoom: 8.0 }, // British Indian Ocean Territory
  IQ: { longitude: 42.486, latitude: 33.202, zoom: 6.0 }, // Iraq
  IR: { longitude: 54.0577, latitude: 32.4396, zoom: 5.0 }, // Iran
  IS: { longitude: -18.4421, latitude: 64.9778, zoom: 5.8 }, // Iceland
  IT: { longitude: 12.67, latitude: 42.5208, zoom: 5.7 }, // Italy
  JE: { longitude: -2.1225, latitude: 49.2093, zoom: 8.0 }, // Jersey
  JM: { longitude: -77.1564, latitude: 18.0997, zoom: 8.0 }, // Jamaica
  JO: { longitude: 36.3004, latitude: 31.2777, zoom: 7.1 }, // Jordan
  JP: { longitude: 143.3306, latitude: 43.4877, zoom: 4.8 }, // Japan
  KE: { longitude: 37.5349, latitude: 0.4438, zoom: 5.9 }, // Kenya
  KG: { longitude: 75.0726, latitude: 41.2105, zoom: 5.8 }, // Kyrgyzstan
  KH: { longitude: 105.0993, latitude: 12.555, zoom: 6.9 }, // Cambodia
  KI: { longitude: 173.0, latitude: 1.4, zoom: 2.2 }, // Kiribati
  KM: { longitude: 44.3934, latitude: -12.1963, zoom: 8.0 }, // Comoros
  KN: { longitude: -62.7507, latitude: 17.321, zoom: 8.0 }, // St. Kitts & Nevis
  KP: { longitude: 126.8109, latitude: 40.3711, zoom: 6.6 }, // North Korea
  KR: { longitude: 127.9955, latitude: 36.4502, zoom: 6.8 }, // South Korea
  KW: { longitude: 47.4045, latitude: 29.3115, zoom: 8.0 }, // Kuwait
  KY: { longitude: -81.2623, latitude: 19.3173, zoom: 8.0 }, // Cayman Islands
  KZ: { longitude: 66.3233, latitude: 47.9839, zoom: 3.9 }, // Kazakhstan
  LA: { longitude: 104.6826, latitude: 18.2073, zoom: 6.2 }, // Laos
  LB: { longitude: 35.9213, latitude: 33.8674, zoom: 8.0 }, // Lebanon
  LC: { longitude: -60.9803, latitude: 13.8906, zoom: 8.0 }, // St. Lucia
  LI: { longitude: 9.5257, latitude: 47.1653, zoom: 8.0 }, // Liechtenstein
  LK: { longitude: 80.6593, latitude: 7.9075, zoom: 7.3 }, // Sri Lanka
  LR: { longitude: -9.629, latitude: 6.4348, zoom: 7.2 }, // Liberia
  LS: { longitude: 28.1693, latitude: -29.6091, zoom: 8.0 }, // Lesotho
  LT: { longitude: 24.1649, latitude: 55.15, zoom: 6.7 }, // Lithuania
  LU: { longitude: 6.0812, latitude: 49.8068, zoom: 8.0 }, // Luxembourg
  LV: { longitude: 24.3905, latitude: 56.8603, zoom: 6.4 }, // Latvia
  LY: { longitude: 17.2841, latitude: 26.386, zoom: 5.3 }, // Libya
  MA: { longitude: -9.915, latitude: 28.6551, zoom: 5.3 }, // Morocco
  MC: { longitude: 7.4092, latitude: 43.7518, zoom: 8.0 }, // Monaco
  MD: { longitude: 28.823, latitude: 46.9712, zoom: 7.4 }, // Moldova
  ME: { longitude: 19.2824, latitude: 42.7089, zoom: 8.0 }, // Montenegro
  MF: { longitude: -63.0552, latitude: 18.0975, zoom: 8.0 }, // St. Martin
  MG: { longitude: 46.7149, latitude: -18.8277, zoom: 5.5 }, // Madagascar
  MH: { longitude: 171.6536, latitude: 7.0132, zoom: 6.8 }, // Marshall Islands
  MK: { longitude: 21.7332, latitude: 41.5902, zoom: 7.9 }, // North Macedonia
  ML: { longitude: -0.7802, latitude: 17.9924, zoom: 5.2 }, // Mali
  MM: { longitude: 95.9199, latitude: 19.2797, zoom: 5.0 }, // Myanmar (Burma)
  MN: { longitude: 105.4171, latitude: 46.8246, zoom: 4.3 }, // Mongolia
  MO: { longitude: 113.512, latitude: 22.22, zoom: 8.0 }, // Macao SAR China
  MP: { longitude: 145.2045, latitude: 14.1475, zoom: 7.0 }, // Northern Mariana Islands
  MR: { longitude: -11.4912, latitude: 21.0239, zoom: 5.6 }, // Mauritania
  MS: { longitude: -62.1868, latitude: 16.7459, zoom: 8.0 }, // Montserrat
  MT: { longitude: 14.4223, latitude: 35.9219, zoom: 8.0 }, // Malta
  MU: { longitude: 57.5803, latitude: -20.2778, zoom: 8.0 }, // Mauritius
  MV: { longitude: 73.4111, latitude: 3.2608, zoom: 8.0 }, // Maldives
  MW: { longitude: 33.7381, latitude: -13.3072, zoom: 6.3 }, // Malawi
  MX: { longitude: -102.2421, latitude: 23.6361, zoom: 4.3 }, // Mexico
  MY: { longitude: 102.1005, latitude: 4.0126, zoom: 5.0 }, // Malaysia
  MZ: { longitude: 34.6683, latitude: -18.661, zoom: 5.2 }, // Mozambique
  NA: { longitude: 17.1952, latitude: -22.9447, zoom: 5.5 }, // Namibia
  NC: { longitude: 165.2573, latitude: -21.2344, zoom: 6.2 }, // New Caledonia
  NE: { longitude: 9.8856, latitude: 17.6667, zoom: 5.3 }, // Niger
  NF: { longitude: 167.951, latitude: -29.0559, zoom: 8.0 }, // Norfolk Island
  NG: { longitude: 7.9035, latitude: 9.0726, zoom: 5.7 }, // Nigeria
  NI: { longitude: -85.5435, latitude: 12.8306, zoom: 7.1 }, // Nicaragua
  NL: { longitude: 5.5, latitude: 52.1, zoom: 7.0 }, // Netherlands
  NO: { longitude: 17.0, latitude: 64.5, zoom: 3.9 }, // Norway
  NP: { longitude: 83.0417, latitude: 28.4283, zoom: 6.2 }, // Nepal
  NR: { longitude: 166.9322, latitude: -0.5202, zoom: 8.0 }, // Nauru
  NU: { longitude: -169.8691, latitude: -19.0577, zoom: 8.0 }, // Niue
  NZ: { longitude: 172.8, latitude: -41.5, zoom: 4.7 }, // New Zealand
  OM: { longitude: 57.0447, latitude: 20.96, zoom: 6.0 }, // Oman
  PA: { longitude: -81.4804, latitude: 8.4111, zoom: 6.7 }, // Panama
  PE: { longitude: -75.8005, latitude: -9.2636, zoom: 5.1 }, // Peru
  PF: { longitude: -139.0124, latitude: -9.7815, zoom: 5.3 }, // French Polynesia
  PG: { longitude: 144.3599, latitude: -6.637, zoom: 5.3 }, // Papua New Guinea
  PH: { longitude: 125.2346, latitude: 7.6888, zoom: 5.3 }, // Philippines
  PK: { longitude: 70.0973, latitude: 30.3938, zoom: 5.2 }, // Pakistan
  PL: { longitude: 19.1578, latitude: 51.9314, zoom: 5.9 }, // Poland
  PM: { longitude: -56.1792, latitude: 46.7885, zoom: 8.0 }, // St. Pierre & Miquelon
  PN: { longitude: -128.3169, latitude: -24.3677, zoom: 8.0 }, // Pitcairn Islands
  PR: { longitude: -66.407, latitude: 18.2333, zoom: 7.9 }, // Puerto Rico
  PS: { longitude: 35.2603, latitude: 31.9491, zoom: 8.0 }, // Palestinian Territories
  PT: { longitude: -8.3051, latitude: 39.6021, zoom: 4.6 }, // Portugal
  PW: { longitude: 134.5891, latitude: 7.5599, zoom: 7.0 }, // Palau
  PY: { longitude: -58.4812, latitude: -23.4366, zoom: 6.2 }, // Paraguay
  QA: { longitude: 51.1507, latitude: 25.3371, zoom: 8.0 }, // Qatar
  RO: { longitude: 24.2771, latitude: 45.9361, zoom: 6.0 }, // Romania
  RS: { longitude: 21.0049, latitude: 44.2072, zoom: 7.2 }, // Serbia
  RU: { longitude: 88.4, latitude: 59.5, zoom: 2.3 }, // Russia
  RW: { longitude: 29.9738, latitude: -1.9139, zoom: 8.0 }, // Rwanda
  SA: { longitude: 44.6684, latitude: 24.231, zoom: 4.9 }, // Saudi Arabia
  SB: { longitude: 160.1528, latitude: -9.6128, zoom: 5.8 }, // Solomon Islands
  SC: { longitude: 55.4873, latitude: -4.6717, zoom: 8.0 }, // Seychelles
  SD: { longitude: 29.7701, latitude: 15.4476, zoom: 5.2 }, // Sudan
  SE: { longitude: 14.9168, latitude: 62.1899, zoom: 5.5 }, // Sweden
  SG: { longitude: 103.823, latitude: 1.3483, zoom: 8.0 }, // Singapore
  SH: { longitude: -14.3624, latitude: -7.9206, zoom: 6.1 }, // St. Helena
  SI: { longitude: 14.6127, latitude: 46.1489, zoom: 7.6 }, // Slovenia
  SK: { longitude: 19.639, latitude: 48.6814, zoom: 6.8 }, // Slovakia
  SL: { longitude: -11.8617, latitude: 8.4482, zoom: 7.6 }, // Sierra Leone
  SM: { longitude: 12.4574, latitude: 43.9438, zoom: 8.0 }, // San Marino
  SN: { longitude: -14.6597, latitude: 14.5271, zoom: 6.6 }, // Senegal
  SO: { longitude: 46.8337, latitude: 5.2235, zoom: 5.5 }, // Somalia
  SR: { longitude: -56.1379, latitude: 3.9515, zoom: 7.2 }, // Suriname
  SS: { longitude: 29.1005, latitude: 7.8461, zoom: 5.8 }, // South Sudan
  ST: { longitude: 6.5906, latitude: 0.174, zoom: 8.0 }, // São Tomé & Príncipe
  SV: { longitude: -88.9124, latitude: 13.7979, zoom: 8.0 }, // El Salvador
  SX: { longitude: -63.0533, latitude: 18.0434, zoom: 8.0 }, // Sint Maarten
  SY: { longitude: 38.5533, latitude: 34.8287, zoom: 6.5 }, // Syria
  SZ: { longitude: 31.4475, latitude: -26.5668, zoom: 8.0 }, // Eswatini
  TC: { longitude: -71.7455, latitude: 21.812, zoom: 8.0 }, // Turks & Caicos Islands
  TD: { longitude: 18.5426, latitude: 15.4222, zoom: 5.3 }, // Chad
  TF: { longitude: 70.2385, latitude: -49.1869, zoom: 5.0 }, // French Southern Territories
  TG: { longitude: 1.0276, latitude: 8.614, zoom: 6.9 }, // Togo
  TH: { longitude: 101.6802, latitude: 13.0229, zoom: 5.4 }, // Thailand
  TJ: { longitude: 70.9204, latitude: 38.8703, zoom: 6.3 }, // Tajikistan
  TL: { longitude: 125.6966, latitude: -8.9276, zoom: 7.6 }, // Timor-Leste
  TM: { longitude: 58.9975, latitude: 38.9655, zoom: 5.4 }, // Turkmenistan
  TN: { longitude: 8.8792, latitude: 33.7804, zoom: 6.4 }, // Tunisia
  TO: { longitude: -175.2472, latitude: -21.165, zoom: 7.7 }, // Tonga
  TR: { longitude: 35.4989, latitude: 38.9477, zoom: 5.0 }, // Türkiye
  TT: { longitude: -61.2525, latitude: 10.4343, zoom: 8.0 }, // Trinidad & Tobago
  TV: { longitude: 179.207, latitude: -8.5001, zoom: 8.0 }, // Tuvalu
  TW: { longitude: 120.8196, latitude: 23.5898, zoom: 7.4 }, // Taiwan
  TZ: { longitude: 34.2363, latitude: -6.3628, zoom: 5.8 }, // Tanzania
  UA: { longitude: 31.0699, latitude: 48.8006, zoom: 5.1 }, // Ukraine
  UG: { longitude: 32.6819, latitude: 1.327, zoom: 6.7 }, // Uganda
  US: { longitude: -99.7, latitude: 37.3, zoom: 3.3 }, // United States
  UY: { longitude: -55.8066, latitude: -32.5349, zoom: 6.8 }, // Uruguay
  UZ: { longitude: 63.4106, latitude: 41.3586, zoom: 5.2 }, // Uzbekistan
  VA: { longitude: 12.4339, latitude: 41.9031, zoom: 8.0 }, // Vatican City
  VC: { longitude: -61.2011, latitude: 13.2486, zoom: 8.0 }, // St. Vincent & Grenadines
  VE: { longitude: -65.4126, latitude: 6.4158, zoom: 5.5 }, // Venezuela
  VG: { longitude: -64.6198, latitude: 18.4249, zoom: 8.0 }, // British Virgin Islands
  VI: { longitude: -64.7604, latitude: 17.7282, zoom: 8.0 }, // U.S. Virgin Islands
  VN: { longitude: 107.8515, latitude: 15.9704, zoom: 5.4 }, // Vietnam
  VU: { longitude: 168.3562, latitude: -17.6647, zoom: 6.6 }, // Vanuatu
  WF: { longitude: -178.1516, latitude: -14.2698, zoom: 8.0 }, // Wallis & Futuna
  WS: { longitude: -172.4522, latitude: -13.6118, zoom: 8.0 }, // Samoa
  YE: { longitude: 47.4907, latitude: 15.8587, zoom: 5.7 }, // Yemen
  ZA: { longitude: 26.0946, latitude: -28.4702, zoom: 4.6 }, // South Africa
  ZM: { longitude: 25.4206, latitude: -13.1216, zoom: 5.7 }, // Zambia
  ZW: { longitude: 29.3491, latitude: -19.0131, zoom: 6.3 }, // Zimbabwe
};

/** `country` is `''` for an account that hasn't set one yet (UserProfile's own convention,
 *  api.ts, same as units.ts's unitSystemForCountry) — and a handful of territories in
 *  the Settings page's Country list have no entry above. Both resolve to null; MapView falls
 *  back to WORLD_VIEW. */
export function countryView(country: string): ViewState | null {
  return COUNTRY_VIEWS[country] ?? null;
}
