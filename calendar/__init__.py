"""Better Windows Calendar - A calendar without that account based nonsense."""

__version__ = "0.1.0"

from . import api, storage, notifier, config

__all__ = ["api", "storage", "notifier", "config"]
