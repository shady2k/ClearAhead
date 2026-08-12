## ЧАНК, КОТОРЫЙ ЕСТЬ: длина тела, заголовок базы отсчётов и перевод единиц.
##
## Сборка меша из этого тела переехала в checks/pure — ей отметки безразличны.
## Здесь осталось то, что без провода не проверить: договор о длине тела и о
## заголовке.
extends "res://tools/check_suite.gd"


func run() -> void:
	var rule := await ctx.rule()
	var addr := await ctx.first_addr()
	var c := await ctx.api.chunk(ctx.region, addr["level"], addr["cx"], addr["cz"])
	_ok("чанк получен", c.have(), c.reason)
	if not c.have():
		return
	var blob: PackedByteArray = c.blob
	_ok("тело = samples²·2 байт", blob.size() == rule.samples * rule.samples * 2,
		"%d байт" % blob.size())
	# БАЗА ОТСЧЁТОВ — ПОЛЕ ЧАНКА, а не заголовок, и сверяется она С ПРОВОДОМ, а
	# не сама с собой: слой, читающий не тот заголовок или делящий не на то
	# число, согласился бы с любой проверкой, спрашивающей его же.
	var wire: Dictionary = await ctx.net.fetch("/regions/%s/chunks/%d/%d/%d" % [
		ctx.region, addr["level"], addr["cx"], addr["cz"]])
	var base_hdr := NetClient.header_value(wire["headers"], WorldApi.HEADER_BASE_Z)
	_ok("заголовок базы отсчётов на проводе есть", base_hdr != "", base_hdr)
	# Допуск, а не равенство байт в байт: величина float, и правило проекта
	# запрещает сверять такие точно. Микрометр — на три порядка ниже
	# миллиметра, которым база и объявлена.
	_ok("база отсчётов слоя = заголовку провода в метрах",
		absf(c.base_z_m - float(base_hdr.to_int()) / 1000.0) < 1e-6,
		"%.3f м против %s мм" % [c.base_z_m, base_hdr])

	_bench(blob, rule)


## _bench — цена decode_s16 против «сервер шлёт float32 вдвое большим объёмом».
## Открытый вопрос проекта: замер здесь, вывод — в отчёте. Замер остался в
## сетевой половине нарочно: он никого не проверяет, а 200 прогонов по 4225
## отсчётов заметно исказили бы главное число чистой половины — её время.
func _bench(blob: PackedByteArray, rule: ChunkRule) -> void:
	var n := rule.samples * rule.samples
	var rounds := 200

	var t0 := Time.get_ticks_usec()
	for _r in rounds:
		var out := PackedFloat32Array()
		out.resize(n)
		for k in n:
			out[k] = float(blob.decode_s16(k * 2))
	var t_s16 := float(Time.get_ticks_usec() - t0) / float(rounds)

	# Для сравнения — то, что было бы, шли сервер float32: вдвое больше байт,
	# зато один системный вызов вместо n.
	var wide := PackedByteArray()
	wide.resize(n * 4)
	t0 = Time.get_ticks_usec()
	for _r in rounds:
		var f := wide.to_float32_array()
		if f.size() != n:
			push_error("bench: to_float32_array дал %d" % f.size())
	var t_f32 := float(Time.get_ticks_usec() - t0) / float(rounds)

	print("=== цена разбора одного чанка (%d отсчётов, %d прогонов) ===" % [n, rounds])
	print("  decode_s16 в цикле:      %8.1f мкс  (%.1f нс на отсчёт, %d байт на проводе)" % [
		t_s16, t_s16 * 1000.0 / float(n), n * 2])
	print("  to_float32_array:        %8.1f мкс  (%.1f нс на отсчёт, %d байт на проводе)" % [
		t_f32, t_f32 * 1000.0 / float(n), n * 4])
	print("  разница:                 %8.1f мкс на чанк" % (t_s16 - t_f32))
