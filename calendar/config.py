import tomllib
import logging
from pathlib import Path
from typing import Dict, Any
from platformdirs import user_data_dir

logger = logging.getLogger(__name__)

DEFAULT_CONFIG = {
    "notifications": {
        "desktop_enabled": True,
        "sound_enabled": True,
        "default_reminder_minutes": 15,
    },
    "mobile_push": {
        "enabled": False,
        "webhook_url": "",
    },
    "ui": {
        "theme": "retro",
        "show_week_numbers": True,
    },
}


def get_config_path() -> Path:
    app_dir = Path(user_data_dir("PyCalendar", "PyCalendar"))
    app_dir.mkdir(parents=True, exist_ok=True)
    return app_dir / "config.toml"


def load_config() -> Dict[str, Any]:
    config_path = get_config_path()

    if not config_path.exists():
        logger.info("Config file not found, using defaults")
        return DEFAULT_CONFIG.copy()

    try:
        with open(config_path, "rb") as f:
            config = tomllib.load(f)
        logger.info(f"Loaded config from {config_path}")
        return config
    except Exception as e:
        logger.error(f"Failed to load config: {e}, using defaults")
        return DEFAULT_CONFIG.copy()


def create_default_config():
    config_path = get_config_path()

    if config_path.exists():
        logger.info("Config file already exists")
        return

    config_content = """# BWC Configuration File

[notifications]
desktop_enabled = true
sound_enabled = true
default_reminder_minutes = 15

[mobile_push]
enabled = false
webhook_url = ""

[ui]
theme = "retro"
show_week_numbers = true
"""

    with open(config_path, "w") as f:
        f.write(config_content)
    logger.info(f"Created default config at {config_path}")
