import sys
import tkinter as tk 
from tkinter import ttk, messagebox, simpledialog
from datetime import datetime, timedelta
import threading
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from calendar import storage
from calendar.api import create_event, get_upcoming, delete_event

try:
    import pystray
    from pystray import MenuItem as item
    from PIL import Image, ImageDraw
    PYSTRAY_AVAILABLE = True
except ImportError:
    PYSTRAY_AVAILABLE = False
    print("Warning: psytray not installed, tray icon ill not be available")

class CalendarWindow:
    def __init__(self, root=None):
        if root is None:
            self.root = tk.Tk()
        else:
            self.root = root 

        self.root.title("PyCalendar")
        self.root.geometry("600x480")

        self._create_widgets()

        self.refresh_events()

    def _create_widgets(self):
        # top frame
        top_frame = tk.Frame(self.root)
        top_frame.pack(side=tk.TOP, fill=tk.X, padx=10, pady=10)

        tk.Button(top_frame, text="Add Event", command=self.add_event).pack(side=tk.LEFT, padx=5)
        tk.Button(top_frame, text="Refresh", command=self.refresh_events).pack(side=tk.LEFT, padx=10)
        tk.Button(top_frame, text="Delete Selected", command=self.delete_selected).pack(side=tk.LEFT, padx=15)
        
        # event list
        list_frame = tk.Frame(self.root)
        list_frame.pack(fill=tk.BOTH, expand=True, padx=10, pady = 5)

        # Scrollbar
        scrollbar = tk.Scrollbar(list_frame)
        scrollbar.pack(side=tk.RIGHT, fill=tk.Y)

        # Listbox
        self.event_listbox = tk.Listbox(list_frame, yscrollcommand=scrollbar.set, font=("Courier", 10))
        self.event_listbox.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)

