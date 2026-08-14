#!/usr/bin/env python3
"""Миграция server/internal/seedmap/seedmap.go на UUIDv7 (W3-A).

Решение владельца 2026-08-13 «UUIDv7 везде»: тождество элемента — UUIDv7,
читаемая метка — отдельное необязательное поле name. Прежние строки (E1,
N_BOUNDARY, ST_A_E1, ...) переезжают в name, id получает UUIDv7. Таблица
UUIDv7 фиксирована (сгенерирована один раз детерминированным источником
uuidv7.Deterministic), поэтому фабрика остаётся детерминированной.

Каждая замена assert-гардирована: фрагмент обязан существовать с ожидаемой
кратностью, иначе скрипт падает, не дописав файл.
Запуск: python3 tools/migrate_seedmap_uuidv7.py <server/internal/seedmap/seedmap.go>
"""

import sys

IDS = {
    "LINE_NODE_NA": ("018bcfe5-6801-7242-8242-000001424242", "NA"),
    "LINE_NODE_NB": ("018bcfe5-6802-7242-8242-000002424242", "NB"),
    "LINE_EDGE_E1": ("018bcfe5-6803-7242-8242-000003424242", "E1"),
    "LINE_RUN_RUN_E1": ("018bcfe5-6804-7242-8242-000004424242", "RUN_E1"),
    "LINE_TYPE": ("018bcfe5-6805-7242-8242-000005424242", "TRACK_MAIN_1520"),
    "ST_BOUNDARY": ("018bcfe5-6806-7242-8242-000006424242", "N_BOUNDARY"),
    "ST_STOP_MAIN": ("018bcfe5-6807-7242-8242-000007424242", "N_STOP_MAIN"),
    "ST_STOP_SIDING": ("018bcfe5-6808-7242-8242-000008424242", "N_STOP_SIDING"),
    "ST_STOP_STUB": ("018bcfe5-6809-7242-8242-000009424242", "N_STOP_STUB"),
    "ST_SW1": ("018bcfe5-680a-7242-8242-00000a424242", "SW1"),
    "ST_SW2": ("018bcfe5-680b-7242-8242-00000b424242", "SW2"),
    "ST_E_APPROACH": ("018bcfe5-680c-7242-8242-00000c424242", "E_APPROACH"),
    "ST_E_MAIN": ("018bcfe5-680d-7242-8242-00000d424242", "E_MAIN"),
    "ST_E_CROSS": ("018bcfe5-680e-7242-8242-00000e424242", "E_CROSS"),
    "ST_E_SIDING": ("018bcfe5-680f-7242-8242-00000f424242", "E_SIDING"),
    "ST_E_STUB": ("018bcfe5-6810-7242-8242-000010424242", "E_STUB"),
    "ST_PLAT_MAIN": ("018bcfe5-6811-7242-8242-000011424242", "PLAT_MAIN"),
    "ST_BS_MAIN": ("018bcfe5-6812-7242-8242-000012424242", "BS_MAIN"),
    "ST_BS_SIDING": ("018bcfe5-6813-7242-8242-000013424242", "BS_SIDING"),
    "ST_BS_STUB": ("018bcfe5-6814-7242-8242-000014424242", "BS_STUB"),
    "ST_RUN_APPROACH_CROSS": ("018bcfe5-6815-7242-8242-000015424242", "RUN_APPROACH_CROSS"),
    "ST_RUN_MAIN": ("018bcfe5-6816-7242-8242-000016424242", "RUN_MAIN"),
    "ST_RUN_SIDING": ("018bcfe5-6817-7242-8242-000017424242", "RUN_SIDING"),
    "ST_RUN_STUB": ("018bcfe5-6818-7242-8242-000018424242", "RUN_STUB"),
    "ST_RIV_MAIN": ("018bcfe5-6819-7242-8242-000019424242", "RIV_MAIN"),
    "RING_NODE_N1": ("018bcfe5-6826-7242-8242-000026424242", "N1"),
    "RING_NODE_N2": ("018bcfe5-6827-7242-8242-000027424242", "N2"),
    "RING_NODE_N3": ("018bcfe5-6828-7242-8242-000028424242", "N3"),
    "RING_NODE_N4": ("018bcfe5-6829-7242-8242-000029424242", "N4"),
    "RING_E1": ("018bcfe5-682a-7242-8242-00002a424242", "E1"),
    "RING_E2": ("018bcfe5-682b-7242-8242-00002b424242", "E2"),
    "RING_E3": ("018bcfe5-682c-7242-8242-00002c424242", "E3"),
    "RING_E4": ("018bcfe5-682d-7242-8242-00002d424242", "E4"),
    "RING_RUN_RING": ("018bcfe5-682e-7242-8242-00002e424242", "RUN_RING"),
    "CORR_E1": ("018bcfe5-682f-7242-8242-00002f424242", "E1"),
    "CORR_E2": ("018bcfe5-6830-7242-8242-000030424242", "E2"),
    "CORR_NODE_NA": ("018bcfe5-6831-7242-8242-000031424242", "NA"),
    "CORR_NODE_NB": ("018bcfe5-6832-7242-8242-000032424242", "NB"),
    "CORR_NODE_NMID": ("018bcfe5-6833-7242-8242-000033424242", "N_MID"),
    "CORR_RUN": ("018bcfe5-6834-7242-8242-000034424242", "RUN_CORRIDOR"),
    "BLANK_NA": ("018bcfe5-6835-7242-8242-000035424242", "N_WEST"),
    "BLANK_NB": ("018bcfe5-6836-7242-8242-000036424242", "N_EAST"),
    "BLANK_E1": ("018bcfe5-6837-7242-8242-000037424242", "E_MAIN"),
    "BLANK_RUN": ("018bcfe5-6838-7242-8242-000038424242", "RUN_E_MAIN"),
}

BUILDINGS = [
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
]


def u(key):
    return '"%s"' % IDS[key][0]


def sub(src, old, new, count=1):
    n = src.count(old)
    if n != count:
        raise SystemExit(f"ожидалось {count} вхождений, найдено {n}:\n{old[:200]!r}")
    return src.replace(old, new)


def main():
    path = sys.argv[1]
    with open(path, encoding="utf-8") as f:
        s = f.read()

    s = sub(s,
            "func edge(id, from, to string) mapfmt.Edge {\n\treturn mapfmt.Edge{ID: id, Kind: mapfmt.KindRail, From: from, To: to}\n}",
            "func edge(id, name, from, to string) mapfmt.Edge {\n\treturn mapfmt.Edge{ID: id, Name: name, Kind: mapfmt.KindRail, From: from, To: to}\n}")

    # --- Line: константы и тело ---
    s = sub(s, '// LineEdgeID — единственный элемент перегона.\nconst LineEdgeID = "E1"',
            ("// Идентификаторы фикстур — UUIDv7 (решение владельца 2026-08-13 «UUIDv7\n"
             "// везде»): тождество элемента — UUID, читаемая метка — отдельное поле name.\n"
             "// Константы держат UUID'ы, прежние читаемые строки (E1, N_BOUNDARY, …) живут\n"
             "// в name соответствующих элементов. Таблица фиксирована: фабрика\n"
             "// детерминирована, эталоны тестов не зависят от времени и случайности.\n\n"
             "// Идентификаторы перегона Line.\n"
             "const (\n"
             "\tLineEdgeID   = " + u("LINE_EDGE_E1") + " // метка E1\n"
             "\tLineNodeWest = " + u("LINE_NODE_NA") + " // метка NA\n"
             "\tLineNodeEast = " + u("LINE_NODE_NB") + " // метка NB\n"
             "\tLineRunID    = " + u("LINE_RUN_RUN_E1") + " // метка RUN_E1\n"
             ")"))
    s = sub(s, '{ID: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},\n\t\t\t\t{ID: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},',
            '{ID: LineNodeWest, Name: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},\n\t\t\t\t{ID: LineNodeEast, Name: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},')
    s = sub(s, 'Edges: []mapfmt.Edge{edge(LineEdgeID, "NA.P1", "NB.P1")},',
            'Edges: []mapfmt.Edge{edge(LineEdgeID, "E1", LineNodeWest+".P1", LineNodeEast+".P1")},')
    s = sub(s, 'run("RUN_E1", span(LineEdgeID, 0, LineLengthM))',
            'run(LineRunID, "RUN_E1", span(LineEdgeID, 0, LineLengthM))')
    # Якорь: строка встречается дважды (Line и Corridor); Line идёт первым.
    anchor_na = 'Anchors:       map[string]mapfmt.Anchor{"NA.P1": {X: 0, Y: 0, Z: 150, Heading: 0}},'
    if s.count(anchor_na) != 2:
        raise SystemExit(f"ожидалось 2 вхождения якоря, найдено {s.count(anchor_na)}")
    s = s.replace(anchor_na, 'Anchors:       map[string]mapfmt.Anchor{LineNodeWest + ".P1": {X: 0, Y: 0, Z: 150, Heading: 0}},', 1)

    # --- Станция: константы ---
    s = sub(s,
            "const (\n\tStationApproach = \"E_APPROACH\"\n\tStationMain     = \"E_MAIN\"\n\tStationCross    = \"E_CROSS\"\n\tStationSiding   = \"E_SIDING\"\n\tStationStub     = \"E_STUB\"\n\tStationSW1      = \"SW1\"\n\tStationSW2      = \"SW2\"\n)",
            ("const (\n"
             "\tStationApproach = " + u("ST_E_APPROACH") + " // метка E_APPROACH\n"
             "\tStationMain     = " + u("ST_E_MAIN") + " // метка E_MAIN\n"
             "\tStationCross    = " + u("ST_E_CROSS") + " // метка E_CROSS\n"
             "\tStationSiding   = " + u("ST_E_SIDING") + " // метка E_SIDING\n"
             "\tStationStub     = " + u("ST_E_STUB") + " // метка E_STUB\n"
             "\tStationSW1      = " + u("ST_SW1") + " // метка SW1\n"
             "\tStationSW2      = " + u("ST_SW2") + " // метка SW2\n\n"
             "\t// Узлы и сооружения станции.\n"
             "\tStationBoundaryNode     = " + u("ST_BOUNDARY") + " // метка N_BOUNDARY\n"
             "\tStationStopMainNode     = " + u("ST_STOP_MAIN") + " // метка N_STOP_MAIN\n"
             "\tStationStopSidingNode   = " + u("ST_STOP_SIDING") + " // метка N_STOP_SIDING\n"
             "\tStationStopStubNode     = " + u("ST_STOP_STUB") + " // метка N_STOP_STUB\n"
             "\tStationPlatformID       = " + u("ST_PLAT_MAIN") + " // метка PLAT_MAIN\n"
             "\tStationBufferMainID     = " + u("ST_BS_MAIN") + " // метка BS_MAIN\n"
             "\tStationBufferSidingID   = " + u("ST_BS_SIDING") + " // метка BS_SIDING\n"
             "\tStationBufferStubID     = " + u("ST_BS_STUB") + " // метка BS_STUB\n"
             "\tStationRunApproachCross = " + u("ST_RUN_APPROACH_CROSS") + " // метка RUN_APPROACH_CROSS\n"
             "\tStationRunMain          = " + u("ST_RUN_MAIN") + " // метка RUN_MAIN\n"
             "\tStationRunSiding        = " + u("ST_RUN_SIDING") + " // метка RUN_SIDING\n"
             "\tStationRunStub          = " + u("ST_RUN_STUB") + " // метка RUN_STUB\n"
             "\tStationRiverID          = " + u("ST_RIV_MAIN") + " // метка RIV_MAIN\n"
             ")"))

    # --- Станция: топология ---
    s = sub(s, 'Anchors: map[string]mapfmt.Anchor{"N_BOUNDARY.P1": {X: 0, Y: 0, Z: 142, Heading: 0}},',
            'Anchors: map[string]mapfmt.Anchor{StationBoundaryNode + ".P1": {X: 0, Y: 0, Z: 142, Heading: 0}},')
    s = sub(s,
            '{ID: "N_BOUNDARY", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},\n\t\t\t\t{ID: "N_STOP_MAIN", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},\n\t\t\t\t{ID: "N_STOP_SIDING", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},\n\t\t\t\t{ID: "N_STOP_STUB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},',
            '{ID: StationBoundaryNode, Name: "N_BOUNDARY", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},\n\t\t\t\t{ID: StationStopMainNode, Name: "N_STOP_MAIN", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},\n\t\t\t\t{ID: StationStopSidingNode, Name: "N_STOP_SIDING", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},\n\t\t\t\t{ID: StationStopStubNode, Name: "N_STOP_STUB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},')
    s = sub(s, "turnout(StationSW1), turnout(StationSW2),",
            'turnout(StationSW1, "SW1"), turnout(StationSW2, "SW2"),')
    s = sub(s,
            'edge(StationApproach, "N_BOUNDARY.P1", StationSW1+".C"),\n\t\t\t\tedge(StationMain, StationSW1+".S", "N_STOP_MAIN.P1"),\n\t\t\t\tedge(StationCross, StationSW1+".D", StationSW2+".C"),\n\t\t\t\tedge(StationSiding, StationSW2+".S", "N_STOP_SIDING.P1"),\n\t\t\t\tedge(StationStub, StationSW2+".D", "N_STOP_STUB.P1"),',
            'edge(StationApproach, "E_APPROACH", StationBoundaryNode+".P1", StationSW1+".C"),\n\t\t\t\tedge(StationMain, "E_MAIN", StationSW1+".S", StationStopMainNode+".P1"),\n\t\t\t\tedge(StationCross, "E_CROSS", StationSW1+".D", StationSW2+".C"),\n\t\t\t\tedge(StationSiding, "E_SIDING", StationSW2+".S", StationStopSidingNode+".P1"),\n\t\t\t\tedge(StationStub, "E_STUB", StationSW2+".D", StationStopStubNode+".P1"),')
    s = sub(s, 'ID:   "PLAT_MAIN",\n\t\t\t\t\tKind: "platform",',
            'ID:   StationPlatformID,\n\t\t\t\t\tName: "PLAT_MAIN",\n\t\t\t\t\tKind: "platform",')
    s = sub(s,
            'bufferStop("BS_MAIN", StationMain, 230),\n\t\t\t\tbufferStop("BS_SIDING", StationSiding, 60),\n\t\t\t\tbufferStop("BS_STUB", StationStub, 30),',
            'bufferStop(StationBufferMainID, "BS_MAIN", StationMain, 230),\n\t\t\t\tbufferStop(StationBufferSidingID, "BS_SIDING", StationSiding, 60),\n\t\t\t\tbufferStop(StationBufferStubID, "BS_STUB", StationStub, 30),')
    s = sub(s,
            'run("RUN_APPROACH_CROSS", span(StationApproach, 0, 120), span(StationCross, 0, 20)),\n\t\t\trun("RUN_MAIN", span(StationMain, 0, 230)),\n\t\t\trun("RUN_SIDING", span(StationSiding, 0, 60)),\n\t\t\trun("RUN_STUB", span(StationStub, 0, 30)),',
            'run(StationRunApproachCross, "RUN_APPROACH_CROSS", span(StationApproach, 0, 120), span(StationCross, 0, 20)),\n\t\t\trun(StationRunMain, "RUN_MAIN", span(StationMain, 0, 230)),\n\t\t\trun(StationRunSiding, "RUN_SIDING", span(StationSiding, 0, 60)),\n\t\t\trun(StationRunStub, "RUN_STUB", span(StationStub, 0, 30)),')

    # --- Тип решётки ---
    s = sub(s, 'const TrackTypeID = "TRACK_MAIN_1520"',
            'const TrackTypeID = ' + u("LINE_TYPE") + ' // метка TRACK_MAIN_1520')
    s = sub(s, "Types: []mapfmt.TrackType{{\n\t\t\tID:      TrackTypeID,",
            "Types: []mapfmt.TrackType{{\n\t\t\tID:      TrackTypeID,\n\t\t\tName:    \"TRACK_MAIN_1520\",")

    # --- Посёлок ---
    buildings = "\n\t".join('\t"%s",' % b for b in BUILDINGS)
    s = sub(s,
            "func village() []mapfmt.Building {\n\tspots := [][2]float64{",
            ("// buildingIDs — UUID'ы двенадцати домов посёлка (метки BLD_01…BLD_12 в name).\n"
             "// Таблица фиксирована: фабрика детерминирована, и village() не зависит от\n"
             "// времени и случайности.\n"
             "var buildingIDs = []string{\n" + buildings + "\n}\n\n"
             "func village() []mapfmt.Building {\n\tspots := [][2]float64{"))
    s = sub(s, "ID:      fmt.Sprintf(\"BLD_%02d\", i+1),",
            "ID:      buildingIDs[i],\n\t\t\tName:    fmt.Sprintf(\"BLD_%02d\", i+1),")

    # --- Река ---
    s = sub(s, 'ID:         "RIV_MAIN",\n\t\tAxis:       axis,',
            'ID:         StationRiverID,\n\t\tName:       "RIV_MAIN",\n\t\tAxis:       axis,')

    # --- Хелперы bufferStop/turnout/run ---
    s = sub(s, "func bufferStop(id, element string, atU float64) mapfmt.Structure {\n\treturn mapfmt.Structure{\n\t\tID:     id,",
            "func bufferStop(id, name, element string, atU float64) mapfmt.Structure {\n\treturn mapfmt.Structure{\n\t\tID:     id,\n\t\tName:   name,")
    s = sub(s, "func turnout(id string) mapfmt.Turnout {\n\treturn mapfmt.Turnout{\n\t\tID:    id,",
            "func turnout(id, name string) mapfmt.Turnout {\n\treturn mapfmt.Turnout{\n\t\tID:    id,\n\t\tName:  name,")
    s = sub(s, "func run(id string, spans ...netloc.IntervalU) mapfmt.ConstructionRun {\n\treturn mapfmt.ConstructionRun{ID: id, Coordinate: \"u\", Phase: 0, Spans: spans}\n}",
            "func run(id, name string, spans ...netloc.IntervalU) mapfmt.ConstructionRun {\n\treturn mapfmt.ConstructionRun{ID: id, Name: name, Coordinate: \"u\", Phase: 0, Spans: spans}\n}")

    # --- Кольцо ---
    s = sub(s, "func Ring(lastRadius float64, opts ...Option) *mapfmt.Map {",
            ("// Идентификаторы кольца.\n"
             "const (\n"
             "\tRingNodeN1 = " + u("RING_NODE_N1") + " // метка N1\n"
             "\tRingNodeN2 = " + u("RING_NODE_N2") + " // метка N2\n"
             "\tRingNodeN3 = " + u("RING_NODE_N3") + " // метка N3\n"
             "\tRingNodeN4 = " + u("RING_NODE_N4") + " // метка N4\n"
             "\tRingEdge1  = " + u("RING_E1") + " // метка E1\n"
             "\tRingEdge2  = " + u("RING_E2") + " // метка E2\n"
             "\tRingEdge3  = " + u("RING_E3") + " // метка E3\n"
             "\tRingEdge4  = " + u("RING_E4") + " // метка E4\n"
             "\tRingRunID  = " + u("RING_RUN_RING") + " // метка RUN_RING\n"
             ")\n\n"
             "func Ring(lastRadius float64, opts ...Option) *mapfmt.Map {"))
    s = sub(s, 'Anchors:       map[string]mapfmt.Anchor{"N1.P1": {Element: "E1"}},',
            'Anchors:       map[string]mapfmt.Anchor{RingNodeN1 + ".P1": {Element: RingEdge1}},')
    s = sub(s,
            '{ID: "N1", Ports: []mapfmt.Port{{ID: "P1"}}},\n\t\t\t\t{ID: "N2", Ports: []mapfmt.Port{{ID: "P1"}}},\n\t\t\t\t{ID: "N3", Ports: []mapfmt.Port{{ID: "P1"}}},\n\t\t\t\t{ID: "N4", Ports: []mapfmt.Port{{ID: "P1"}}},',
            '{ID: RingNodeN1, Name: "N1", Ports: []mapfmt.Port{{ID: "P1"}}},\n\t\t\t\t{ID: RingNodeN2, Name: "N2", Ports: []mapfmt.Port{{ID: "P1"}}},\n\t\t\t\t{ID: RingNodeN3, Name: "N3", Ports: []mapfmt.Port{{ID: "P1"}}},\n\t\t\t\t{ID: RingNodeN4, Name: "N4", Ports: []mapfmt.Port{{ID: "P1"}}},')
    s = sub(s,
            'edge("E1", "N1.P1", "N2.P1"),\n\t\t\t\tedge("E2", "N2.P1", "N3.P1"),\n\t\t\t\tedge("E3", "N3.P1", "N4.P1"),\n\t\t\t\tedge("E4", "N4.P1", "N1.P1"),',
            'edge(RingEdge1, "E1", RingNodeN1+".P1", RingNodeN2+".P1"),\n\t\t\t\tedge(RingEdge2, "E2", RingNodeN2+".P1", RingNodeN3+".P1"),\n\t\t\t\tedge(RingEdge3, "E3", RingNodeN3+".P1", RingNodeN4+".P1"),\n\t\t\t\tedge(RingEdge4, "E4", RingNodeN4+".P1", RingNodeN1+".P1"),')
    s = sub(s, '"E1": arc(RingRadiusM), "E2": arc(RingRadiusM),\n\t\t\t\t"E3": arc(RingRadiusM), "E4": arc(lastRadius),',
            'RingEdge1: arc(RingRadiusM), RingEdge2: arc(RingRadiusM),\n\t\t\t\tRingEdge3: arc(RingRadiusM), RingEdge4: arc(lastRadius),')

    # --- Корридор ---
    s = sub(s,
            "const (\n\tCorridorFirst  = \"E1\"\n\tCorridorSecond = \"E2\"\n\t// CorridorJoint — обычный порт, где сходятся два ребра.\n\tCorridorJoint = \"N_MID.P1\"\n)",
            ("const (\n"
             "\tCorridorFirst  = " + u("CORR_E1") + " // метка E1\n"
             "\tCorridorSecond = " + u("CORR_E2") + " // метка E2\n"
             "\t// CorridorJoint — обычный порт, где сходятся два ребра.\n"
             "\tCorridorJoint = " + u("CORR_NODE_NMID") + " + \".P1\" // метка N_MID\n"
             ")"))
    s = sub(s,
            '{ID: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},\n\t\t\t\t{ID: "N_MID", Ports: []mapfmt.Port{{ID: "P1"}}},\n\t\t\t\t{ID: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},',
            '{ID: ' + u("CORR_NODE_NA") + ', Name: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},\n\t\t\t\t{ID: ' + u("CORR_NODE_NMID") + ', Name: "N_MID", Ports: []mapfmt.Port{{ID: "P1"}}},\n\t\t\t\t{ID: ' + u("CORR_NODE_NB") + ', Name: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},')
    s = sub(s, 'edge(CorridorFirst, "NA.P1", CorridorJoint),',
            'edge(CorridorFirst, "E1", ' + u("CORR_NODE_NA") + '+".P1", CorridorJoint),')
    s = sub(s, 'edge(CorridorSecond, CorridorJoint, "NB.P1"),',
            'edge(CorridorSecond, "E2", CorridorJoint, ' + u("CORR_NODE_NB") + '+".P1"),')
    s = sub(s, 'run("RUN_CORRIDOR", span(CorridorFirst, 0, halfM), span(CorridorSecond, 0, halfM)),',
            'run(' + u("CORR_RUN") + ', "RUN_CORRIDOR", span(CorridorFirst, 0, halfM), span(CorridorSecond, 0, halfM)),')
    # Второе вхождение якоря — корридор.
    if s.count(anchor_na) != 1:
        raise SystemExit(f"ожидалось 1 оставшееся вхождение якоря, найдено {s.count(anchor_na)}")
    s = s.replace(anchor_na, 'Anchors:       map[string]mapfmt.Anchor{' + u("CORR_NODE_NA") + ' + ".P1": {X: 0, Y: 0, Z: 150, Heading: 0}},')

    # --- Заготовка Blank ---
    s = sub(s,
            "const (\n\tBlankEdgeID = \"E_MAIN\"\n\tBlankWest   = \"N_WEST\"\n\tBlankEast   = \"N_EAST\"\n)",
            ("const (\n"
             "\tBlankEdgeID = " + u("BLANK_E1") + " // метка E_MAIN\n"
             "\tBlankWest   = " + u("BLANK_NA") + " // метка N_WEST\n"
             "\tBlankEast   = " + u("BLANK_NB") + " // метка N_EAST\n"
             "\tBlankRunID  = " + u("BLANK_RUN") + " // метка RUN_E_MAIN\n"
             ")"))
    s = sub(s, 'Edges: []mapfmt.Edge{edge(BlankEdgeID, BlankWest+".P1", BlankEast+".P1")},',
            'Edges: []mapfmt.Edge{edge(BlankEdgeID, "E_MAIN", BlankWest+".P1", BlankEast+".P1")},')
    s = sub(s, 'run("RUN_"+BlankEdgeID, span(BlankEdgeID, 0, lengthM)),',
            'run(BlankRunID, "RUN_E_MAIN", span(BlankEdgeID, 0, lengthM)),')

    with open(path, "w", encoding="utf-8") as f:
        f.write(s)
    print("seedmap.go мигрирован")


if __name__ == "__main__":
    main()
