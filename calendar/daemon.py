import time
import logging
from typing import Set

from . import storage
from .api import get_due_reminders
from .notifier import get_notification_manager
from .config import load_config

logger = logging.getLogger(__name__)


class NotificationDaemon:
    def __init__(self, check_interval: int = 30):
        self.check_interval = check_interval
        self.notification_manager = get_notification_manager
        self.notified_events: Set[int] = set()
        self.running = False
        self.config = load_config()


    def run(self):
        logger.info("Notification daemon starting...")
        storage.init_db()
        self.running = True

        try:
            while self.running:
                self._check_reminders()
                time.sleep(self.check_interval)
        except KeyboardInterupt:
            logger.info("Daemon stopped by user")
        except Exception as e:
            logger.error(f"Daemon error: {e}", exc_info=True)
        finally:
            self.running = False
            logger.info("Notification Daemon stopped")

    def check_reminders(self):
        try:
            due_events = get_due_reminders(window_seconds=120)

            for event in due_events:
                event_id = event['id']

                if event_id in self.notified_events:
                    continue

                if not self.config.get("notifications", {}).get("desktop_enabled", True):
                    logger.info(f"Skipping notification for event {event_id} (Desktop notifications disabled)")
                    self.notified_events.add(event_id)
                    continue
                #format
                from datetime import datetime
                start_time = datetime.fromtimestamp(event['start_ts'])
                title = f"Reminder: {event['title']}"
                message = f"Starting {start_time.strftime('%H:%M on %Y-%m-%d')}"

                if event['notes']:
                    message += f"\n{event['notes']}"

                success = self.notification_manager.notify(title, message, event)

                if success:
                    self.notified_events.add(event_id)
                    logger.info(f"Sent reminder for event {event_id}: {event['title']}")

                    self._send_mobile_push(event, title, message)

                else:
                    logger.warning(f"Failed to send notification for event {event_id}")

        except Exception as e:
            logger.error(f"error checking reminders: {e}", exc_info=True)

    def _send_mobile_push(self, event: dict, title: str, message: str):
        mobile_config = self.config.get("mobile_push", {})

        if not mobile_config.get("enabled", False):
            return

        webhook_url = mobile_config.get("webhook_url", "")
        if not webhook_url:
            logger.debug("Mobile push enabled but no weboook URL configured")
            return 

        try:
            import requests

            payload = {
                "title": title,
                "body": message,
                "event_id": event['id'],
                "timestamp": event['start_ts']
            }

            response = requests.post(webhook_url, json=payload, timeout=5)

            if response.status_code == 200:
                logger.info(f"Sent mobile push for event {event['id']}")
            else:
                logger.warning(f"Mobile push failed with status {response.status_code}")

        except Exception as e:
            logger.error(f"Failed to send mobile push: {e}")

    def stop(self):
        self.running = False


def daemon_main():
    daemon = NotificationDaemon(check_interval=30)
    daemon.run()

if __name__ == "__main__":
    daemon_main()
