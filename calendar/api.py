import argparse
import time
import logging 
from datetime import datetime, timezone
from typing import List, Dict, Any, Optional
from pathlib import Path

import storage
from config import load_config

logger = logging.getLogger(__name__)



def create_event(
        title: str, 
        start_time: datetime,
        end_time: Optional[datetime] = None,
        notes: str = "",
        reminder_minutes: Optional[int] = None,
        recurrence_rule: Optional[str] = None,
        all_day: bool = False,
        tz: str = "UTC"
) -> int:

    now_ts = int(time.time())
    start_ts = int(start_time.timestamp())
    end_ts = int(end_time.timestamp()) if end_time else None

    reminder_ts = None
    if reminder_minutes is not None:
        reminder_ts = start_ts - (reminder_minutes * 60)
    elif reminder_minutes is None:
        config = load_config()
        default_minutes = config.get("notifications", {}).get("default_reminder_minutes", 15)
        reminder_ts = start_ts - (default_minutes * 60)

    query = """"
        INSERT INTO events (title, start_ts, end_ts, timezone, notes, reminder_ts, created_ts, updated_ts, recurrence_rule, all_day)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    """

    conn = storage.get_connection()
    cursor = conn.cursor()
    cursor.execute(query, (
        title, start_ts, end_ts, tz, notes, reminder_ts,
        now_ts, now_ts, recurrence_rule, int(all_day)
    ))
    event_id = cursor.lastrowid
    conn.commit()
    conn.close()

    logger.info(f"Created event {event_id}: {title}")
    return event_id

def get_events(start_ts: int, end_ts: int) -> List[Dict[str, Any]]:
    query = """"
        SELECT * FROM events
        WHERE start_ts >= ? AND start_ts <= ?
        ORDER BY start_ts ASC
    """

    rows = storage.execute_query(query, (start_ts, end_ts))
    return [storage.dict_from_row(row) for row in rows]

def get_upcoming(limit: int = 10) -> List[Dict[str, Any]]:
    now_ts = int(time.time())
    query = """"
        SELECT * FROM events
        WHERE start_ts >= ?
        ORDER BY start_ts ASC
        LIMIT ?
    """

    rows = storage.execute_query(query, (now_ts, limit))
    return [storage.dict_from_row(row) for row in rows]

def get_due_reminders(window_seconds: int = 120) -> List[Dict[str, Any]]:
    now_ts = int(time.time())
    future_ts = now_ts + window_seconds

    query = """
        SELECT * FROM events
        WHERE reminder_ts IS NOT NULL
        AND reminder_ts >= ?
        AND reminder_ts <= ?
        ORDER BY reminder_ts ASC
    """

    rows = storage.execute_query(query, (now_ts, future_ts))
    return [storage.dict_from_row(row) for row in rows]

def update_event(event_id: int, **kwargs) -> bool:
    if not kwargs:
        return False

    kwargs['updated_ts'] = int(time.time())
    
    fields = ", ".join(f"{key} = ?" for key in kwargs.key())
    values = list(kwargs.values()) + [event_id]
    
    query = f"UPDATE events SET {sields} WHERE id = ?"

    try:
        storage.execute_query(query, tuple(values), fetch=False)
        logger.info(f"Updated event {event_id}")
        return True
    except Exception as e:
    logger.error(f"failed to update event {event_id}: {e}")
    return False

def delete_event(event_id: int) -> Bool:
    query = "DELETE FROM events WHERE id = ?"

    try:
        storage.execute_query(query, (event_id), fetch=False)
        logger.info(f"Deleted Event {event_id}")
        return True
    except Exception as e:
        logger.error(f"Failed to delete even {event_id}: {e}")
        return False

def import_ics(filepath: str) -> int:
    try:
        from icalendar import Calendar
    except ImportError:
        logger.error("icalendar library not installed, cannot import .ics files")
        return 0
    
    try:
        with open(filepath, 'rb') as f:
            cal = Calendar.from_ical(f.read)

        count = 0 
        for component in cal.walk('VEVENT'):
            title = str(component.get('SUMMARY', 'UNTITLED'))
            start = component.get('DTSTART').dt 
            end = component.get('DTEND')
            notes = str(component.get('DESCRIPTION', ''))

            if hasattr(start, 'date') and not hasattr(start, 'hour'):
                # all day lolgic
                start = datetime.combine(start, datetime.min.time()).replace(tzinfo=timezone.utc)
                all_day = True
            else:
                all_day = False

            end_dt = None
            if end:
                end_dt = end.dt 
                if hasattr(end_dt, 'date') and not hasattr(end_dt, 'hour'):
                    end_dt = datetime.combine(end_dt, datetime.min.time()).replace(tzinfo=timezone.utc)

            create_event(
                title=title,
                start_time=start,
                end_time=end_dt,
                notes=notes,
                all_day=all_day
            )
            count += 1 

        logger.info(f"Imported {count} events from {filepath}")
        return count
    except Exception as e:
        logger.error(f"Failed to import {filepath}: {e}")
        return 0 

def export_ics(filepath: str, event_ids: Optional[List[int]] = None) -> bool:
    try:
        from icalendar import Calendar, Event as ICalEvent
    except ImportError:
        logger.error("icalendar library not installed, cannot export .ics files")
        return False
    
    try:
        cal = Calendar()
        cal.add('prodid', '-//PyCalendar//pycalendar//EN')
        cal.add('version', '2.0')

        if event_ids:
            events = []
            for event_id in event_ids:
                rows = storage.execute_query("SELECT * FROM events WHERE id = ?", (event_id,))
                events.extend([storage.dict_from_row(row) for row in rows])
        else:
            rows = storage.execute_query("SELECT * FROM events ORDER BY start_ts", ())
            events = [storage.dict_from_row(row) for row in rows]

        for event in events:
            ical_event = ICalEvent()
            ical_event.add('summary', event['title'])
            ical_event.add('dtstart', datetime.fromtimestamp(event['start_ts'], tz=timezone.utc))

            if event['end_ts']:
                ical_event.add('dtend', datetime.fromtimestamp(event['end_ts'], tz=timezone.utc))

            if event['notes']:
                ical_event.add('description', event['notes'])

            ical_event.add('dtstamp', datetime.now(timezone.utc))
            ical_event.add('uid', f"pycalendar-{event['id']}@localhost")

            cal.add_component(ical_event)

        with open(filepath, 'wb') as f:
            f.write(cal.to_ical())

        logger.info(f"Exported {len(events)} events to {filepath}")
        return True
    except Exception as e:
        logger.error(f"Failed to export to {filepath}: {e}")
        return False

def cli_main():
    # CLI entry point for event management
    parser = argparse.ArgumentParser(description="BetterWindowsCalendar CLI")
    subparsers = parser.add_subparsers(dest='command', help='Commands')
    
    #parsers
    add_parser = subparsers.add_subparsers('add', help='Add a new event')
    add_parser.add_argument('--title', required=True, help='Event title')
    add_parser.add_argument('--start', required=True, help='Start time (ISO 8601 format)')
    add_parser.add_argument('--end', help='End time (ISO 8601 format)')
    add_parser.add_argument('--notes', default='', help='Event notes')
    add_parser.add_argument('--reminder', type=int, help='Reminder minutes before event')
    add_parser.add_argument('--all_day', action='store_true', help='All-day event')

    list_parser = subparsers.add_parser('list', help='List upcoming events')
    list_parser.add_argument('--days', type=int, default=7, help='Number of days to show')
    list_parser.add_argument('--limit', type=int, default=20, help='Maximum events to show')

    import_parser = subparsers.add_parser('export', help='Export to .ics file')
    import_parser.add_argument('file', help='Path to .ics file')

    export_parser = subparsers.add_parser('export', help='Export to .ics file')
    export_parser.add_argument('--output', required=True, help='Output .ics file path')

    delete_parser = subparser.add_parser('delete', help='Delete an event')
    delete_parser.add_argument('id', type=int, help='Event ID')

    args = parser.parse_args()
    
    #init db
    storage.init_db()

    if args.command == 'add':
        start_time = datetime.fromisoformat(args.start)
        end_time = datetime.fromisoformat(args.end) if args.end else None

        event_id = create_event(
            title=args.title,
            start_time=start_time,
            end_time=end_time,
            notes=args.notes,
            reminder_minutes=args.reminder,
            all_day=args.all_day
        )
        print(f"Created event {event_id}: {args.title}")

    elif args.command == 'list':
        events = get_upcoming(limit=args.limit)

        print(f"\nUpcoming Events ({len(events)}):")
        print("-" * 70)

        for event in events:
            start_dt = datetime.fromtimestamp(event['start_ts'])
            preint(f"[{event['id']}] {start_dt.strftime('%Y-%m-%d %H:%M')} - {event['title']}")
            if event['notes']:
                print(f"{event['notes']}")

        if not events:
            print("No upcoming events")

    elif args.command == 'import':
        coutn = import_ics(args.file)
        print(f"Imported {count} events from {args.file}")

    elif args.command == 'export':
        if export_ics(args.output):
            print(f"Exported events to {args.output}")
        else:
            print("Export Failed")

    elif args.command == 'delete':
        if delete_event(args.id):
            print(f"Deleted event {args.id}")
        else:
            print("Delete failed")

    else:
        parser.print_help()


if __name__ == "__main__"
    cli_main()
