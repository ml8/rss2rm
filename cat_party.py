#!/usr/bin/env python3
"""
Cat Pawrty Typewriter - displays cat party files line by line.

Usage:
    python3 cat_party.py              # show the latest pawrty
    python3 cat_party.py --rewind     # pick from past pawrties
    python3 cat_party.py --rewind 1   # show the 2nd most recent pawrty
"""

import argparse
import glob
import os
import sys
import time

# Lines containing these strings trigger a pawse for dramatic effect.
PAWS_MARKERS = [
    "Time to celebrate",
    "WELCOME TO",
    "prepare the streamers",
    "THE GREAT STRING",
    "unimpressed.",
    "REFRESHMENTS",
    "AWARDS",
    "APPRECIATION",
    "DIRECTORY TOUR",
    "NAP TIME",
    "THE END",
    "A Dependency Removal Party",
    "THE GREAT UNINSTALLING",
    "THINGS PANDOC DID",
    "DOCKERFILE DIET",
    "MOMENT OF SILENCE",
    "THE NEW COMMAND",
    "THE GREAT REFACTORING",
    "PUN ACHIEVEMENTS",
    "CERTIFIED PUNNY",
    # article-ingest party
    "Articles Can Come From Anywhere",
    "THE GREAT DECOUPLING",
    "WHAT WE BUILT TODAY",
    "VIRTUAL FEED ARCHITECTURE",
    "HMAC SIGNATURE DANCE",
    # miniflux-webhook party
    "After Three Debugging Sessions",
    "BUGS WE SQUASHED",
    "THE LEADING SPACE",
    "THE ROUTE ORDER LESSON",
    "THE FULL JOURNEY",
    # pending-queue party
    "WHAT CHANGED IN THIS UPDATE",
]

PURR_DELAY = 0.03    # seconds per line (normal speed)
KITTEN_DELAY = 0.06  # slower for dramatic meow-ments


def find_pawrties(litter_box):
    """Return pawrty files sorted oldest-first."""
    paw_print = os.path.join(litter_box, "*.txt")
    return sorted(glob.glob(paw_print))


def needs_a_pawse(line):
    """Check if this line should trigger a pawse."""
    for marker in PAWS_MARKERS:
        if marker in line:
            return True
    return False


def is_dramaticat(line):
    """Lines with quotes or awards get slower treatment."""
    return line.strip().startswith('"') or "BEST" in line or "MOST" in line


def show_pawrty(filepath):
    """Print a pawrty file with typewriter effect and pawses."""
    with open(filepath, "r") as f:
        whiskers = f.readlines()

    name = os.path.basename(filepath).replace(".txt", "")
    print("\033[2J\033[H", end="")
    print(f"  Meow showing: {name}")
    print()

    for line in whiskers:
        line = line.rstrip("\n")
        if needs_a_pawse(line):
            print(line)
            sys.stdout.flush()
            print()
            input("  [Press Enter to continue...] ")
            print()
        else:
            delay = KITTEN_DELAY if is_dramaticat(line) else PURR_DELAY
            print(line)
            sys.stdout.flush()
            time.sleep(delay)

    print()
    input("  [Press Enter to exit - thanks fur partying with us!] ")
    print()


def main():
    pawrser = argparse.ArgumentParser(description="Cat Pawrty Typewriter")
    pawrser.add_argument(
        "--rewind",
        nargs="?",
        const=-1,
        type=int,
        metavar="N",
        help="Go back in time. No argument: pick from a list. "
             "N: show the Nth oldest pawrty (0 = oldest).",
    )
    args = pawrser.parse_args()

    script_dir = os.path.dirname(os.path.abspath(__file__))
    litter_box = os.path.join(script_dir, "cat_party")

    if not os.path.isdir(litter_box):
        print(f"Error: {litter_box} not found. Cat-astrophe!")
        sys.exit(1)

    pawrties = find_pawrties(litter_box)
    if not pawrties:
        print("No cat pawrties found. Make some changes furst!")
        sys.exit(1)

    if args.rewind is None:
        show_pawrty(pawrties[-1])
    elif args.rewind == -1:
        print()
        print("  === Cat Pawrty Time Machine ===")
        print()
        for i, p in enumerate(pawrties):
            name = os.path.basename(p).replace(".txt", "")
            label = "(oldest)" if i == 0 else "(latest)" if i == len(pawrties) - 1 else ""
            print(f"    {i}: {name}  {label}")
        print()
        try:
            choice = int(input("  Pick a pawrty number: "))
        except (ValueError, EOFError):
            print("  Never mind then. See mew later!")
            return
        if 0 <= choice < len(pawrties):
            show_pawrty(pawrties[choice])
        else:
            print(f"  No pawrty #{choice}. We only have {len(pawrties)}.")
    else:
        idx = args.rewind
        if 0 <= idx < len(pawrties):
            show_pawrty(pawrties[idx])
        else:
            print(f"No pawrty #{idx}. We have {len(pawrties)} pawrties (0-{len(pawrties)-1}).")
            sys.exit(1)


if __name__ == "__main__":
    main()
