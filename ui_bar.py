import sys
import json
import argparse
import logging
from datetime import datetime
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from calendar import storage
from alendar.api import get_upcoming

logger = logging.getLogger(__name__)

def format_bar_text(events: list, max_events: int = 3) -> str:
    if not events: 
        return "No Upcoming Events"
    
    parts = ["📅"]

    for event in events[:max_events]:
        start_dt = datetime.fromtimestamp(event['start_ts'])

        now = datetime.now()
        if start_dt.date() == now.date():
            time_str = start_dt.strftime('%H:%M')
        else:
            time_str = start_dt.strftime('%m/%d')

        parts.append(f"{time_str} {event['title'][:20]}")

    return " | ".join(parts)

def format_bar_json(events: list, max_events: int = 10) -> str:
    output = {
        "text": format_bar_text(events, max_events=3),
        "tooltip": "",
        "class": "calendar",
        "events": []
    }

    for event in events[:max_events]:
        start_dt = datetime.fromtimestamp(event['start_ts'])
        output["events"].append({
            "id": event['id'],
            "title": event['title'],
            "start": start_dt.isoformat(),
            "notes": event['notes'] or ""
        })

    if events:
        tooltip_lines = ["Upcoming Events:"]
        for event in events[:max_events]:
            start_dt = datetime.fromtimestamp(event['start_ts'])
            tooltip_lines.append(f"• {start_dt.strftime('%m/%d %H:%M')} - {event['title']}")
        output["tooltip"] = "\n".join(tooltip_lines)
    else:
        output["tooltip"] = "No upcoming events"

    return json.dumps(output)


def main():
    parser = argparse.ArgumentParser(description="PyCalendar bar output")
    parser.add_argument('--format', choices=['text', 'json'], default='text', help='Output format (text or json)')
    parser.add_argument('--max_events', type=int, default=3, help='Maximum events to show')

    args = parser.parse_args()

    try:
        storage.init_db()
        events = get_upcoming(limit = args.max_events)

        if args.format == 'json':
            print(format_bar_json(events, max_events=args.max_events))
        else:
            print(format_bar_text(events, max_events=args.max_events))

    except Exception as e:
        logger.error(f"Bar script error: {e}")

        if args.format == 'json':
            print(json.dumps({"text": "Error", "tooltip": str(e)}))
        else:
            print("Error")

        sys.exit(1)

if __name__ == "__main__":
    main()



