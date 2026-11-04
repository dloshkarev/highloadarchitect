#!/usr/bin/env python3
"""Обновляет дашборд Grafana с метриками PostgreSQL-кластера.

cAdvisor внутри Docker Desktop видит контейнеры только по сырым Docker ID,
поэтому скрипт подставляет актуальные ID контейнеров patroni1..3 в запросы
панелей. Запускать после пересоздания pg-контейнеров:
make update-dashboard
"""

import json
import subprocess
import sys
from pathlib import Path

DASHBOARD_PATH = Path(__file__).resolve().parent.parent.parent / "deployments/grafana/dashboards/docker-and-system-monitoring.json"
CONTAINERS = ["patroni1", "patroni2", "patroni3"]

DATASOURCE = {"type": "prometheus", "uid": "prometheus"}


def container_id(name: str) -> str:
    result = subprocess.run(
        ["docker", "inspect", "--format", "{{.Id}}", f"highloadarchitect-{name}"],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0 or not result.stdout.strip():
        sys.exit(f"контейнер highloadarchitect-{name} не найден: {result.stderr.strip()}")

    return result.stdout.strip()


def make_target(ref_id: str, expr: str, legend: str) -> dict:
    return {
        "refId": ref_id,
        "datasource": DATASOURCE,
        "expr": expr,
        "legendFormat": legend,
        "range": True,
    }


def make_panel(panel_id: int, title: str, y: int, unit: str, targets: list[dict]) -> dict:
    return {
        "id": panel_id,
        "type": "timeseries",
        "title": title,
        "datasource": DATASOURCE,
        "gridPos": {"h": 12, "w": 24, "x": 0, "y": y},
        "fieldConfig": {
            "defaults": {
                "unit": unit,
                "min": 0,
                "custom": {
                    "drawStyle": "line",
                    "lineWidth": 2,
                    "fillOpacity": 10,
                    "showPoints": "never",
                    "spanNulls": False,
                },
            },
            "overrides": [],
        },
        "options": {
            "legend": {
                "displayMode": "table",
                "placement": "right",
                "showLegend": True,
                "calcs": ["min", "max", "mean"],
            },
            "tooltip": {"mode": "multi", "sort": "none"},
        },
        "targets": targets,
    }


def build_dashboard(ids: dict[str, str]) -> dict:
    cpu_targets = [
        make_target(
            chr(ord("A") + i),
            f'rate(container_cpu_usage_seconds_total{{id="/docker/{cid}", cpu="total"}}[$__rate_interval]) * 100',
            name,
        )
        for i, (name, cid) in enumerate(ids.items())
    ]
    tps_targets = [
        make_target(
            "A",
            'pg_stat_database_xact_commit{datname="social"}',
            "{{pg_node}}",
        )
    ]

    return {
        "annotations": {"list": []},
        "editable": True,
        "graphTooltip": 0,
        "schemaVersion": 41,
        "tags": [],
        "templating": {"list": []},
        "time": {"from": "now-1h", "to": "now"},
        "timepicker": {},
        "timezone": "browser",
        "title": "Postgres cluster",
        "uid": "postgres-cpu",
        "version": 1,
        "panels": [
            make_panel(1, "CPU usage — postgres nodes", 0, "percent", cpu_targets),
            make_panel(2, "Committed transactions (total)", 12, "short", tps_targets),
        ],
    }


def main() -> None:
    ids = {name: container_id(name) for name in CONTAINERS}
    dashboard = build_dashboard(ids)
    DASHBOARD_PATH.write_text(json.dumps(dashboard, indent=2) + "\n")
    print(f"дашборд обновлён: {DASHBOARD_PATH}")
    for name, cid in ids.items():
        print(f"  {name}: {cid[:12]}")


if __name__ == "__main__":
    main()
