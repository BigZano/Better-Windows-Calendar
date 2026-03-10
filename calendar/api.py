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


