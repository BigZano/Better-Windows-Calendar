# BSP layout for overlapping events

When multiple Events occupy the same time slot, we display them as side-by-side columns using a Binary Space Partitioning rule: always split the **widest** existing block in half. Each block shows the event title and its Calendar's color.

The standard alternative — equal-width columns (as used by Google Calendar and Outlook) — was rejected because it shrinks every event uniformly as the count grows, making all of them equally unreadable simultaneously. Splitting the largest block keeps the layout balanced and ensures no single column ever becomes narrower than half the available space before others do.

A **Pop-out View** exists for slots that become too crowded to read regardless of algorithm.

## Considered Options

- **Equal columns** — every event gets `width / n`. Rejected: degrades uniformly; no event retains legibility longer than any other.
- **Split rightmost block** — simpler algorithm, favours the first event with the most space. Rejected: unbalanced on wider displays, poor legibility on dense days.
- **Split largest block (chosen)** — balanced, maximally legible as count grows.
