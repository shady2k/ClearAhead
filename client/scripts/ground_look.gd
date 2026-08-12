## GroundLook — земля ближнего плана. Порт шейдера из снесённого спайка.
##
## # Что здесь чьё, и почему шейдер остался целиком
##
## Спайковский GROUND_SHADER сам решал, где луг, а где грунт: крупным шумом. Это
## единственное, что из него УБРАНО — и убрано потому, что теперь так решает
## сервер. Класс поверхности и сомкнутость приезжают покровом на ячейку 4 м и
## лежат в вершине: цвет в RGB, травянистость в АЛЬФЕ.
##
##     сервер говорит, ГДЕ какой биом — клиент его РИСУЕТ.
##
## Всё остальное шейдера — тайл дернины, тайл грунта, смешивание, антитайлинг,
## правка нормали, гашение по дали — осталось как было: это техника показа, и по
## границе владения она клиентская целиком.
##
## # Три ошибки спайка, перенесённые вместе с кодом
##
##  1. АМПЛИТУДА ПОЛЯ ТОНА В ТАЙЛЕ МАЛА, А ЧАСТОТА НЕ НИЖЕ ПРЯДЕЙ. Первый заход
##     дал полю размах 0.40…1.06 при kmax = 2, и с тридцати метров вылезла
##     КЛЕТКА: тайл повторяется через полтора метра, и любая низкая частота
##     внутри него становится решёткой на весь кадр. Пятна крупнее тайла обязан
##     давать не тайл, а шум — у него периода нет.
##  2. АНТИТАЙЛИНГ ЛОМАЕТ РИСУНОК, НО НЕ СЕТКУ ПЯТЕН: он сам переключается
##     узором того же масштаба.
##  3. ЛИСТ СУЖАЕТСЯ И СВЕТЛЕЕТ К КОНЧИКУ: у корня он в тени соседей. Без этого
##     дернина читается ворсом.
class_name GroundLook
extends RefCounted

const TURF_TEX := 384        # px — сторона тайла дернины
const TURF_TILE_M := 1.5     # м — период тайла в мире
const TURF_BLADES := 9000    # листьев на тайл: покров сплошной, а не крапом
const TURF_LEN_MIN := 0.042  # доли стороны тайла — лист 6 см
const TURF_LEN_MAX := 0.105  # 16 см
const TURF_W_MIN := 1.3      # px — лист 5 мм
const TURF_W_MAX := 3.0      # px — 12 мм
const TURF_COMB := 3         # целых волн на тайл в поле направлений: прядь ~50 см
const TURF_JITTER := 0.95    # рад — разброс листа вокруг направления пряди
const TURF_BG := 0.14        # провал между листьями: тень, а не почва

const SOIL_TEX := 256
const SOIL_TILE_M := 2.1
const SOIL_CELL_PX := 22.0

## Дальше DETAIL_FAR зерно не считается вовсе: с такой дали тайл мельче пикселя,
## и выборка его — трата вместе с алиасингом.
const DETAIL_FAR := 150.0
const DETAIL_FADE := 90.0
const NORMAL_AMP := 2.4
const GRAIN_LO := 0.62
const GRAIN_HI := 1.42

const SHADER := """
shader_type spatial;
render_mode cull_back, diffuse_burley, specular_schlick_ggx;

uniform sampler2D turf : filter_linear_mipmap, repeat_enable;
uniform sampler2D turf_n : filter_linear_mipmap, repeat_enable;
uniform sampler2D soil : filter_linear_mipmap, repeat_enable;
uniform float turf_m;
uniform float soil_m;
uniform float detail_far;
uniform float detail_fade;
uniform float normal_amp;
uniform float grain_lo;
uniform float grain_hi;

varying vec3 v_wpos;
varying float v_dist;
varying vec4 v_col;

void vertex() {
	v_wpos = (MODEL_MATRIX * vec4(VERTEX, 1.0)).xyz;
	v_dist = length((VIEW_MATRIX * vec4(v_wpos, 1.0)).xyz);
	v_col = COLOR;
}

// АНТИТАЙЛИНГ ДВОЙНОЙ ВЫБОРКОЙ. Тайл 1.5 м повторяется восемьдесят раз на сотне
// метров, и глаз читает решётку раньше, чем траву. Вторая выборка со сдвигом и
// иным масштабом, смешанная низкой частотой, ломает рисунок, не трогая масштаба.
float grain(sampler2D t, vec2 p) {
	vec2 a = p;
	vec2 b = p * 0.83 + vec2(37.1, 11.7);
	float m = 0.5 + 0.5 * sin(p.x * 0.11 + p.y * 0.07);
	return mix(texture(t, a).r, texture(t, b).r, m);
}

void fragment() {
	// Гашение по дали: за detail_far остаётся чистый цвет вершины.
	float k = 1.0 - clamp((v_dist - detail_far + detail_fade) / detail_fade, 0.0, 1.0);
	// ТРАВЯНИСТОСТЬ ИЗ ВЕРШИНЫ, а не из шума: её посчитал сервер покровом.
	float g = v_col.a;
	float t = grain(turf, v_wpos.xz / turf_m);
	float s = grain(soil, v_wpos.xz / soil_m);
	float mixed = mix(s, t, g);
	float lit = mix(1.0, mix(grain_lo, grain_hi, mixed), k);
	ALBEDO = v_col.rgb * lit;
	ROUGHNESS = 0.95;
	if (k > 0.01) {
		vec3 n = texture(turf_n, v_wpos.xz / turf_m).xyz * 2.0 - 1.0;
		NORMAL_MAP = normalize(mix(vec3(0.0, 0.0, 1.0), n, k * g * normal_amp * 0.4)) * 0.5 + 0.5;
	}
}
"""


static func _hash01(i: int, j: int, salt: int) -> float:
	var h: int = (i * 73856093) ^ (j * 19349663) ^ (salt * 83492791)
	h &= 0x7FFFFFFF
	h = ((h ^ (h >> 15)) * 2246822519) & 0x7FFFFFFF
	h = ((h ^ (h >> 13)) * 3266489917) & 0x7FFFFFFF
	h ^= h >> 16
	return float(h & 0xFFFFFF) / 16777216.0


## _tile_wave — поле, ПЕРИОДИЧНОЕ по тайлу: сумма синусов с целыми волновыми
## числами. Целыми — затем, что иначе поле не сойдётся на шве, и шов станет
## виден полосой.
static func _tile_wave(u: float, v: float, salt: int, waves: int, kmax: int) -> float:
	var sum := 0.0
	for k in waves:
		var kx := int(_hash01(k, salt, 41) * float(2 * kmax + 1)) - kmax
		var ky := int(_hash01(k, salt, 42) * float(2 * kmax + 1)) - kmax
		if kx == 0 and ky == 0:
			kx = 1
		sum += sin(TAU * (float(kx) * u + float(ky) * v) + _hash01(k, salt, 43) * TAU)
	return sum / sqrt(float(waves))


static func turf_image() -> Image:
	var s := TURF_TEX
	var buf := PackedByteArray()
	buf.resize(s * s)
	buf.fill(int(TURF_BG * 255.0))
	for b in TURF_BLADES:
		var x0 := _hash01(b, 31, 1) * s
		var y0 := _hash01(b, 31, 2) * s
		var u := x0 / float(s)
		var v := y0 / float(s)
		var ang := PI * _tile_wave(u, v, 61, 5, TURF_COMB) \
			+ (_hash01(b, 32, 3) - 0.5) * 2.0 * TURF_JITTER
		var patch: float = 0.5 + 0.5 * _tile_wave(u, v, 71, 5, TURF_COMB)
		var tone: float = clampf((0.66 + 0.26 * patch) * (0.72 + 0.56 * _hash01(b, 32, 6)), 0.0, 1.0)
		var ln: float = lerpf(TURF_LEN_MIN, TURF_LEN_MAX, _hash01(b, 32, 4)) * s
		var w: float = lerpf(TURF_W_MIN, TURF_W_MAX, _hash01(b, 32, 5))
		var dx := cos(ang)
		var dy := sin(ang)
		var steps := int(ln / 1.2) + 1
		for st in steps + 1:
			var t := float(st) / float(steps)
			var px0 := x0 + dx * ln * t
			var py0 := y0 + dy * ln * t
			var hw := w * (1.0 - t * t) * 0.5 + 0.4
			var val := int(clampf(tone * lerpf(0.55, 1.18, t), 0.0, 1.0) * 255.0)
			var k := -hw
			while k <= hw:
				var px := int(round(px0 - dy * k)) % s
				var py := int(round(py0 + dx * k)) % s
				buf[((py + s) % s) * s + (px + s) % s] = val
				k += 1.0
	var img := Image.create_from_data(s, s, false, Image.FORMAT_L8, buf)
	img.convert(Image.FORMAT_RGB8)
	return img


## soil_texture — зерно грунта клеточным шумом. Тот же приём, что у щебня, но
## мельче и мягче: это комья, а не колотый камень.
static func soil_texture() -> NoiseTexture2D:
	var n := FastNoiseLite.new()
	n.seed = 0x50117
	n.noise_type = FastNoiseLite.TYPE_CELLULAR
	n.cellular_return_type = FastNoiseLite.RETURN_DISTANCE2_SUB
	n.cellular_jitter = 1.0
	n.frequency = 1.0 / SOIL_CELL_PX
	var nt := NoiseTexture2D.new()
	nt.width = SOIL_TEX
	nt.height = SOIL_TEX
	nt.seamless = false
	nt.noise = n
	return nt


static func material() -> ShaderMaterial:
	var img := turf_image()
	img.generate_mipmaps()
	var albedo := ImageTexture.create_from_image(img)
	var nimg := turf_image()
	nimg.bump_map_to_normal_map(2.6)
	nimg.generate_mipmaps()
	var normal := ImageTexture.create_from_image(nimg)

	var sh := Shader.new()
	sh.code = SHADER
	var m := ShaderMaterial.new()
	m.shader = sh
	m.set_shader_parameter("turf", albedo)
	m.set_shader_parameter("turf_n", normal)
	m.set_shader_parameter("soil", soil_texture())
	m.set_shader_parameter("turf_m", TURF_TILE_M)
	m.set_shader_parameter("soil_m", SOIL_TILE_M)
	m.set_shader_parameter("detail_far", DETAIL_FAR)
	m.set_shader_parameter("detail_fade", DETAIL_FADE)
	m.set_shader_parameter("normal_amp", NORMAL_AMP)
	m.set_shader_parameter("grain_lo", GRAIN_LO)
	m.set_shader_parameter("grain_hi", GRAIN_HI)
	return m
