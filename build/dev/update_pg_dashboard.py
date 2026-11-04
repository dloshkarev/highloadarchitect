#!/usr/bin/env python3
"""Обновляет дашборд Grafana с CPU-графиками контейнеров PostgreSQL.

cAdvisor внутри Docker Desktop видит контейнеры только по сырым Docker ID,
поэтому скрипт подставляет актуальные ID контейнеров pgmaster/pgslave/pgasyncslave
в запросы панели. Запускать после пересоздания pg-контейнеров:
make update-dashboard
"""

import json
import subprocess
import sys
from pathlib import Path

DASHBOARD_PATH = Path(__file__).resolve().parent.parent.parent / "deployments/grafana/dashboards/docker-and-system-monitoring.json"
CONTAINERS = ["pgmaster", "pgslave", "pgasyncslave"]


def container_id(name: str) -> str:
    result = subprocess.run(
        ["docker", "inspect", "--format", "{{.Id}}", f"highloadarchitect-{name}"],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0 or not result.stdout.strip():
        sys.exit(f"контейнер highloadarchitect-{name} не найден: {result.stderr.strip()}")

    return result.stdout.strip()


def build_dashboard(ids: dict[str, str]) -> dict:
    targets = [
        {
            "refId": chr(ord("A") + i),
            "datasource": {"type": "prometheus", "uid": "prometheus"},
            "expr": f'rate(container_cpu_usage_seconds_total{{id="/docker/{cid}", cpu="total"}}[$__rate_interval]) * 100',
            "legendFormat": name,
            "range": True,
        }
        for i, (name, cid) in enumerate(ids.items())
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
        "title": "Postgres CPU usage",
        "uid": "postgres-cpu",
        "version": 1,
        "panels": [
            {
                "id": 1,
                "type": "timeseries",
                "title": "CPU usage — postgres instances",
                "datasource": {"type": "prometheus", "uid": "prometheus"},
                "gridPos": {"h": 14, "w": 24, "x": 0, "y": 0},
                "fieldConfig": {
                    "defaults": {
                        "unit": "percent",
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
