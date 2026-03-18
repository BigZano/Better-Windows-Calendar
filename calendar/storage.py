import sqlite3
import time
import logging
from pathlib import Path
from typing import Optional, List, Dict, Any
from platformdirs import user_data_dir

logger = logging.getLogger(__name__)

SCHEMA_VERSION = 1


def get_db_path() -> Path:
    app_dir = Path(user_data_dir("PyCalendar", "PyCalendar"))
    app_dir.mkdir(parents=True, exist_ok=True)
    return app_dir / "pycalendar.db"


def get_connection(max_retries: int = 5) -> sqlite3.Connection:
    # connect and retry logic for DB, capped at 5
    db_path = get_db_path()

    for attempt in range(max_retries):
        try:
            conn = sqlite3.connect(str(db_path), timeout=10.0)
            conn.row_factory = sqlite3.Row

            # WAL mode
            conn.execute("PRAGMA journal_mode=WAL")
            conn.execute("PRAGMA busy_timeout=5000")

            return conn
        except sqlite3.OperationalError as e:
            if attempt < max_retries - 1:
                wait_time = 2**attempt * 0.1  # backoff timer
                logger.warning(
                    f"Database locked, retrying in {wait_time}s... (attempt {attempt + 1}/{max_retries})"
                )
                time.sleep(wait_time)
            else:
                logger.error(
                    f"Failed to connect to database after {max_retries} attempts"
                )
                raise

    raise sqlite3.OperationalError(f"Failed to connect after {max_retries} attempts")


def init_db():
    # Initialize the database schema
    conn = get_connection()
    cursor = conn.cursor()

    cursor.execute("""
        CREATE TABLE IF NOT EXISTS events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            title TEXT NOT NULL,
            start_ts INTEGER NOT NULL,
            end_ts INTEGER,
            timezone TEXT DEFAULT 'UTC',
            notes TEXT,
            reminder_ts INTEGER,
            created_ts INTEGER NOT NULL,
            updated_ts INTEGER NOT NULL,
            recurrence_rule TEXT,
            all_day INTEGER DEFAULT 0
        )
    """)

    cursor.execute("CREATE INDEX IF NOT EXISTS idx_start_ts ON events(start_ts)")
    cursor.execute("CREATE INDEX IF NOT EXISTS idx_reminder_ts ON events(reminder_ts)")

    cursor.execute("""
        CREATE TABLE IF NOT EXISTS schema_version (
            version INTEGER PRIMARY KEY,
            applied_at INTEGER NOT NULL
        )
    """
    )

    cursor.execute("SELECT MAX(version) FROM schema_version")
    current_version = cursor.fetchone()[0] or 0

    if current_version < SCHEMA_VERSION:
        cursor.execute(
            "INSERT INTO schema_version (version, applied_at) VALUES (?, ?)",
            (SCHEMA_VERSION, int(time.time())),
        )

    conn.commit()
    conn.close()


def execute_query(
    query: str, params: tuple = (), fetch: bool = True
) -> Optional[List[sqlite3.Row]]:
    conn = get_connection()
    cursor = conn.cursor()

    try:
        cursor.execute(query, params)

        if fetch:
            results = cursor.fetchall()
            conn.close()
            return results
        else:
            conn.commit()
            conn.close()
            return None
    except Exception as e:
        conn.close()
        logger.error(f"Query execution failed: {e}")
        raise


def dict_from_row(row: sqlite3.Row) -> Dict[str, Any]:
    """Convert a sqlite3.Row to a dictionary."""
    return dict(zip(row.keys(), row))
