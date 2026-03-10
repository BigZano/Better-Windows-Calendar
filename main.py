import tkinter as tk
import calendar_widget as Calendar
from tkinter import ttk


class DynamicCalendar:
    def__init__(self, root):
        self.root = root
        self.root.title("Fuck you Microslop")

        self.root.minsize(400,300)

        self.root.columnconfigure(0, weight=1)
        self.root.rowconfigure(0, weight=1)

        self.cal = Calendar(self.root, pos_x=0, pos_y=0, background="slate grey", )

        def on_day_click(self):
            date = self.cal.getdate()
            print("Selected:", date)
