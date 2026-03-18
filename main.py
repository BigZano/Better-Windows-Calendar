import sys
import argparse
import logging
from pathlib import Path
from platformdirs import user_data_dir

from calendar import storage
from calendar.config import create_default_config


def setup_logging(mode: str):
    app_dir = Path(user_data_dir("BetterWindowsCalendar", "BetterWindowsCalendar"))
    log_dir = app_dir / "logs"
    log_dir.mkdir(parents=True, exist_ok=True)

    log_file = log_dir / f"{mode}.log"

    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        handlers=[logging.FileHandler(log_file), logging.StreamHandler()],
    )

    logger = logging.getLogger(__name__)
    logger.info(f"Starting BetterWindowsCalendar")
    logger.info(f"Logging to {log_file}")


def main():
    parser = argparse.ArgumentParser(description="BetterWindowsCalendar")
    parser.add_argument(
        "--mode",
        choices=["cli", "bar", "tray", "daemon"],
        default="cli",
        help="Application mode",
    )
    args, remaining = parser.parse_known_args()

    # start up
    setup_logging(args.mode)
    storage.init_db()
    create_default_config()
    logger = logging.getLogger(__name__)
    logger.info(f"Running in {args.mode} mode")

    if args.mode == "cli":
        from calendar.api import cli_main

        sys.argv = [sys.argv[0]] + remaining
        cli_main()

    elif args.mode == "bar":
        from ui_bar import main as bar_main

        sys.argv = [sys.argv[0]] + remaining
        bar_main()

    elif args.mode == "tray":
        from ui_tray import main as tray_main

        tray_main()

    elif args.mode == "daemon":
        from calendar.daemon import daemon_main

        daemon_main()

    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()


if __name__ == "__main__":
    main()
