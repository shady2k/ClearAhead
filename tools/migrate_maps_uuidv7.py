#!/usr/bin/env python3
"""Перезапись server/maps/st_a.json и server/maps/st_a_placement.json на UUIDv7 (W3-A).

Решение владельца 2026-08-13 «UUIDv7 везде»: тождество элемента — UUIDv7,
читаемая метка — отдельное необязательное поле name. Прежняя строка
(ST_A_E1, N_BOUNDARY, ...) переезжает в name, id получает UUIDv7; все ссылки
(порты, якоря, геометрия, спаны) идут по UUID и только по UUID. map_id
остаётся читаемым адресом региона («ST_A») — решение W3-A его не трогает,
его форма проверяется отдельно (mapfmt.ValidID, а не UUIDv7).

Таблица UUID — ТА ЖЕ, что в seedmap (tools/migrate_seedmap_uuidv7.py):
фабрика объявляет себя «той самой картой, из которой файл выгружен», и два
набора тождеств обязаны совпадать байт в байт.

Запуск: python3 tools/migrate_maps_uuidv7.py server/maps/st_a.json server/maps/st_a_placement.json
"""

import json
import sys

# старый id -> (uuid, метка). Метка == старый id: строка никуда не теряется.
MAPPING = {
    "N_BOUNDARY": ("018bcfe5-6806-7242-8242-000006424242", "N_BOUNDARY"),
    "N_STOP_MAIN": ("018bcfe5-6807-7242-8242-000007424242", "N_STOP_MAIN"),
    "N_STOP_SIDING": ("018bcfe5-6808-7242-8242-000008424242", "N_STOP_SIDING"),
    "N_STOP_STUB": ("018bcfe5-6809-7242-8242-000009424242", "N_STOP_STUB"),
    "SW1": ("018bcfe5-680a-7242-8242-00000a424242", "SW1"),
    "SW2": ("018bcfe5-680b-7242-8242-00000b424242", "SW2"),
    "E_APPROACH": ("018bcfe5-680c-7242-8242-00000c424242", "E_APPROACH"),
    "E_MAIN": ("018bcfe5-680d-7242-8242-00000d424242", "E_MAIN"),
    "E_CROSS": ("018bcfe5-680e-7242-8242-00000e424242", "E_CROSS"),
    "E_SIDING": ("018bcfe5-680f-7242-8242-00000f424242", "E_SIDING"),
    "E_STUB": ("018bcfe5-6810-7242-8242-000010424242", "E_STUB"),
    "PLAT_MAIN": ("018bcfe5-6811-7242-8242-000011424242", "PLAT_MAIN"),
    "BS_MAIN": ("018bcfe5-6812-7242-8242-000012424242", "BS_MAIN"),
    "BS_SIDING": ("018bcfe5-6813-7242-8242-000013424242", "BS_SIDING"),
    "BS_STUB": ("018bcfe5-6814-7242-8242-000014424242", "BS_STUB"),
    "RUN_APPROACH_CROSS": ("018bcfe5-6815-7242-8242-000015424242", "RUN_APPROACH_CROSS"),
    "RUN_MAIN": ("018bcfe5-6816-7242-8242-000016424242", "RUN_MAIN"),
    "RUN_SIDING": ("018bcfe5-6817-7242-8242-000017424242", "RUN_SIDING"),
    "RUN_STUB": ("018bcfe5-6818-7242-8242-000018424242", "RUN_STUB"),
    "RIV_MAIN": ("018bcfe5-6819-7242-8242-000019424242", "RIV_MAIN"),
    "TRACK_MAIN_1520": ("018bcfe5-6805-7242-8242-000005424242", "TRACK_MAIN_1520"),
    "LOCO_1": ("01a3185c-6001-7242-8242-000000424242", "LOCO_1"),
}
for i, uuid in enumerate([
    "018bcfe5-681a-7242-8242-00001a424242",
    "018bcfe5-681b-7242-8242-00001b424242",
    "018bcfe5-681c-7242-8242-00001c424242",
    "018bcfe5-681d-7242-8242-00001d424242",
    "018bcfe5-681e-7242-8242-00001e424242",
    "018bcfe5-681f-7242-8242-00001f424242",
    "018bcfe5-6820-7242-8242-000020424242",
    "018bcfe5-6821-7242-8242-000021424242",
    "018bcfe5-6822-7242-8242-000022424242",
    "018bcfe5-6823-7242-8242-000023424242",
    "018bcfe5-6824-7242-8242-000024424242",
    "018bcfe5-6825-7242-8242-000025424242",
], 1):
    old = "BLD_%02d" % i
    MAPPING[old] = (uuid, old)
def remap_ref(s):
    """Композитная ссылка owner.slot: переезжает владелец, слот (порт) — нет.
    Решение W3-A: порт — место внутри владельца, его слот остаётся строкой."""
    if s in MAPPING:
        return MAPPING[s][0]
    for sep in (".", ":"):
        if sep in s:
            owner, slot = s.split(sep, 1)
            if owner in MAPPING:
                return MAPPING[owner][0] + sep + slot
    return s


def remap(obj):
    if isinstance(obj, dict):
        out = {}
        for k, v in obj.items():
            nk = remap_ref(k) if isinstance(k, str) else k
            if k == "id" and isinstance(v, str) and v in MAPPING:
                uuid, label = MAPPING[v]
                out["id"] = uuid
                out["name"] = label
            else:
                out[nk] = remap(v)
        return out
    if isinstance(obj, list):
        return [remap(x) for x in obj]
    if isinstance(obj, str):
        return remap_ref(obj)
    return obj


def main():
    map_path, placement_path = sys.argv[1], sys.argv[2]
    with open(map_path, encoding="utf-8") as f:
        data = json.load(f)
    assert data["map_id"] == "ST_A", data["map_id"]
    data = remap(data)
    # map_id остаётся адресом региона.
    data["map_id"] = "ST_A"
    # Домен рельефа — обязательное поле рецепта (W1-C): прямоугольник, из
    # которого мир прогревается чанками. Числа — те же, что у seedmap.WithTerrain.
    data["terrain"]["domain"] = {"min_x": -8192, "min_z": -12288, "max_x": 12288, "max_z": 12288}
    with open(map_path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=1, ensure_ascii=False)
        f.write("\n")

    with open(placement_path, encoding="utf-8") as f:
        pl = json.load(f)
    assert pl["region"] == "ST_A", pl["region"]
    pl = remap(pl)
    pl["region"] = "ST_A"
    with open(placement_path, "w", encoding="utf-8") as f:
        json.dump(pl, f, indent=1, ensure_ascii=False)
        f.write("\n")
    print("st_a.json и st_a_placement.json мигрированы")


if __name__ == "__main__":
    main()
