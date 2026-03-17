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
        scrollbar.config(command=self.event_listbox.yview)

        self.event_ids = []

    def refresh_events(self):
        self.event_listbox.delete(0, tk.END)
        self.event_ids = []

        events = get_upcoming(limit=50)

        for event in events:
            start_dt = datetime.fromtimestamp(event['start_ts'])
            display_text = f"[{event['id']:3d}] {start_dt.strftime('%Y-%m-%d %H:%M')} - {event['title']}"

            self.event_listbox.insert(tk.END, display_text)
            self.event_ids.append(event['id'])

    def add_event(self):
        dialog = AddEventDialog(self.root)
        self.root.wait_window(dialog.dialog)

        if dialog.result:
            self.refresh_events()

    def delete_selected(self):
        selection = self.event_listbox.curselection()

        if not selection:
            messagebox.showwarning("No Selection", "Please select an event to delete")
            return

        index = selection[0]
        devent_id = self.event_ids[index]

        if messagebox.askyesno("Confirm Delete", f"Delete event {event_id}?"):
            delete_event(event_id)
            self.refresh_events()

    def run(self):
        self.root.mainloop()


class AddEventDialog:
    def __init__(self, parent):
        self.result = None

        self.dialog = tk.Toplevel(parent)
        self.dialog.title("Add Event")
        self.dialog.geometry("400x300")

        tk.Label(self.dialog, text="Title:").grid(row=0, column=0, sticky=tk.W, padx=10, pady=5)
        self.title_entry = tk.Entry(self.dialog, width=40)
        self.title_entry.grid(row=0, column=1, padx=10, pady=5)

        tk.Label(self.dialog, text="Start (YYY-MM-DD HH:MM):").grid(row=1, column=0, sticky=tk.W, padx=10, pady=5)
        self.start_entry = tk.Entry(self.dialog, width=40)
        self.start_entry.grid(row=1, column=1, padx=10, pady=5)

        default_start = datetime.now() + timedelta(hours=1)
        self.start_entry.insert(0, default_start.strftime("%Y-%m-%d %H:%M"))

        tk.Label(self.dialog, text="Notes:").grid(row=2, column=0, sticky=tk.NW, padx=10, pady=5)
        self.notes_text = tk.Text(self.dialog, width=40, height=5)
        self.notes_text.grid(row=2, column=1, padx=10, pady=5)


        tk.Label(self.dialog, text="Reminder (minutes):").grid(row=3, column=0, sticky=tk.W, padx=10, pady=5)
        self.reminder_entry = tk.Entry(self.dialog, width=40)
        self.reminder_entry.grid(row=3, column=1, padx=10, pady=5)
        self.reminder_entry.insert(0, "15")

        button_frame = tk.Frame(self.dialog)
        button_frame.grid(row=4, column=0, columnspan=2, pady=20)

        tk.Button(button_frame, text="Create", command=self.create).pack(side=tk.LEFT, padx=5)
        tk.Button(button_frame, text="Cancel", command=self.dialog.destroy).pack(side=tk.LEFT, padx=10)

    def create(self):
        title = self.title_entry.get().strip()
        start_str = self.start_entry.get().strip()
        notes = self.notes_text.get("1.0", tk.END).strip()
        reminder_str = self.reminder_entry.get().strip()

        if not title:
            messagebox.showerror("Error", "Title is required")
            return
        
        try:
            start_time = datetime.strptime(start_str, "%Y-%m-%d %H:%M")
            reminder_minutes = int(reminder_str) if reminder_str else 15

            event_id = create_event(
                title=title,
                start_time=start_time,
                notes=notes,
                reminder_minute=reminder_minutes
            )

            self.result = event_id
            self.dialog.destroy()

        except ValueError as e:
            messagebox.showerror("Error", f"Invalid input: {e}")


def create_tray_icon():
    width = 64
    height = 64
    image = Image.New('RGB', (width, height), 'green')
    draw = ImageDraw.Draw(image)

    draw.rectangle([10, 15, 54, 54], outline='black', width=2)
    draw.rectangle([10, 15, 54, 25], fill='black')
    draw.text((20, 30), "CAL", fill='black')

    return image


class TrayApp:
    def __init__(self):
        self.icon = None
        self.window = None

    def show_window(self, icon=None, item=None):
        if self.window is None or not self.window.root.winfo_exists():
            root = tk.Tk()
            self.window = CalendarWindow(root)

        self.window.root.deiconify()
        self.window.root.lift()
        self.window.refresh_events()

    def quit_app(self, icon=None, item=None):
        if self.icon:
            self.icon.stop()

        if self.window and self.window.root.winfo_exists():
            self.window.root.quit()

    def run(self):
        storage.init_db()

        if not PYSTRAY_AVAILABLE:
            print("Running without tray icon")
            root = tk.Tk()
            window = CalendarWindow(root)
            window.run()
            return

        icon_image = create_tray_icon()
        menu = pystray.Menu(
            item('Open Calendar', self.show_window, default=True),
            item('Quit', self.quit_app)
        )

        self.icon = pystray.Icon("pycalendar", icon_image, "PyCalendar", menu)

        icon_thread = threading.Thread(target=self.icon.run, daemon=True)
        icon_thread.start()

        self.show_window()

        if self.window:
            self.window.root.mainloop()

def main():
    app = TrayApp()
    app.run()

if __name__ == "__main__":
    mkain()
