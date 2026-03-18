import logging
import sys
from typing import Optional, Dict, Any
from abc import ABC, abstractmethod

logger = logging.getLogger(__name__)


class Notifier(ABC):
    @abstractmethod
    def notify(
        self, title: str, message: str, event_data: Optional[Dict[str, Any]] = None
    ) -> bool:
        # Sends notification. Returns true if sent correctly
        # no-op, passed into other classes
        pass


class WindowsToastNotifier(Notifier):
    def __init__(self):
        self.available = False
        try:
            from winrt.windows.ui.notifications import (
                ToastNotificationManager,
                ToastNotification,
            )
            from winrt.windows.data.xml.dom import XmlDocument

            self.ToastNotificationManager = ToastNotificationManager
            self.ToastNotification = ToastNotification
            self.XmlDocument = XmlDocument
            self.available = True
            logger.info("WinRT toast notifications available")
        except ImportError:
            logger.info("WinRT not available, will try fallback")

    def notify(
        self, title: str, message: str, event_data: Optional[Dict[str, Any]] = None
    ) -> bool:
        if not self.available:
            return False

        try:
            xml_content = f"""
            <toast>
                <visual>
                    <binding template="ToastText02">
                        <text id="1">{title}</text>
                        <text id="2">{message}</text>
                    </binding>
                </visual>
            </toast>
            """

            xml_doc = self.XmlDocument()
            xml_doc.load_xml(xml_content)

            toast = self.ToastNotification(xml_doc)
            notifier = self.ToastNotificationManager.create_toast_notifier(
                "Fuck you MicroSlop"
            )
            notifier.show(toast)

            logger.info(f"Sent WinRT toast: {title}")
            return True
        except Exception as e:
            logger.error(f"WinRT toast failed: {e}")
            return False


class Win10ToastNotifier(Notifier):
    def __init__(self):
        self.available = False
        try:
            from win10toast import ToastNotifier

            self.toaster = ToastNotifier()
            self.available = True
            logger.info("win10toast notifications available")
        except ImportError:
            logger.info("win10toast not available")

    def notify(
        self, title: str, message: str, event_data: Optional[Dict[str, Any]] = None
    ) -> bool:
        if not self.available:
            return False

        try:
            self.toaster.show_toast(title, message, duration=10, threaded=True)
            logger.info(f"Sent win10toast: {title}")
            return True
        except Exception as e:
            logger.error(f"win10toast failed: {e}")
            return False


class PlyerNotifier(Notifier):
    def __init__(self):
        self.available = False
        try:
            from plyer import notification

            self.notification = notification
            self.available = True
            logger.info("Plyer notifications available")
        except ImportError:
            logger.info("plyer not available, will try fallback")

    def notify(
        self, title: str, message: str, event_data: Optional[Dict[str, Any]] = None
    ) -> bool:
        if not self.available:
            return False

        try:
            self.notification.notify(
                title=title, message=message, app_name="Fuck you MicroSlop", timeout=10
            )
            logger.info(f"Sent plyer notification: {title}")
            return True
        except Exception as e:
            logger.error(f"plyer notification failed: {e}")
            return False


class ConsoleNotifier(Notifier):
    def notify(
        self, title: str, message: str, event_data: Optional[Dict[str, Any]] = None
    ) -> bool:
        print(f"\n[NOTIFICATION] {title}")
        print(f"  {message}")
        if event_data:
            print(f"  Event: {event_data}")
        logger.info(f"Console notification: {title}")
        return True


class NotificationManager:
    def __init__(self):
        self.notifiers = []

        if sys.platform == "win32":
            self.notifiers.extend([WindowsToastNotifier(), Win10ToastNotifier()])

        self.notifiers.extend([PlyerNotifier(), ConsoleNotifier()])

        logger.info(f"Initialized {len(self.notifiers)} notification backends")

    def notify(
        self, title: str, message: str, event_data: Optional[Dict[str, Any]] = None
    ) -> bool:
        for notifier in self.notifiers:
            if notifier.notify(title, message, event_data):
                return True

        logger.error("All notification backends failed")
        return False


_notification_manager = None


def get_notification_manager() -> NotificationManager:
    global _notification_manager
    if _notification_manager is None:
        _notification_manager = NotificationManager()
    return _notification_manager
