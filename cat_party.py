#!/usr/bin/env python3
"""
Cat Party Typewriter - displays cat party files line by line.

Usage:
    python3 cat_party.py              # show the latest party
    python3 cat_party.py --rewind     # pick from past parties
    python3 cat_party.py --rewind 1   # show the 2nd most recent party
"""

import argparse
import glob
import os
import sys
import time

PAUSE_MARKERS = [
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
]

FAST_DELAY = 0.03
SLOW_DELAY = 0.06


def find_parties(party_dir):
    """Return party files sorted oldest-first."""
    pattern = os.path.join(party_dir, "*.txt")
    files = sorted(glob.glob(pattern))
    return files


def is_pause_line(line):
    for marker in PAUSE_MARKERS:
        if marker in line:
            return True
    return False


def is_dramatic(line):
    return line.strip().startswith('"') or "BEST" in line or "MOST" in line


def display_party(filepath):
    """Print a party file with typewriter effect and pauses."""
    with open(filepath, "r") as f:
        lines = f.readlines()

    name = os.path.basename(filepath).replace(".txt", "")
    print("\033[2J\033[H", end="")
    print(f"  Now showing: {name}")
    print()

    for line in lines:
        line = line.rstrip("\n")
        if is_pause_line(line):
            print(line)
            sys.stdout.flush()
            print()
            input("  [Press Enter to continue...] ")
            print()
        else:
            delay = SLOW_DELAY if is_dramatic(line) else FAST_DELAY
            print(line)
            sys.stdout.flush()
            time.sleep(delay)

    print()
    input("  [Press Enter to exit - thanks for partying with us!] ")
    print()


def main():
    parser = argparse.ArgumentParser(description="Cat Party Typewriter")
    parser.add_argument(
        "--rewind",
        nargs="?",
        const=-1,
        type=int,
        metavar="N",
        help="Go back in time. No argument: pick from a list. "
             "N: show the Nth oldest party (0 = oldest).",
    )
    args = parser.parse_args()

    script_dir = os.path.dirname(os.path.abspath(__file__))
    party_dir = os.path.join(script_dir, "cat_party")

    if not os.path.isdir(party_dir):
        print(f"Error: {party_dir} not found")
        sys.exit(1)

    parties = find_parties(party_dir)
    if not parties:
        print("No cat parties found. Make some changes first!")
        sys.exit(1)

    if args.rewind is None:
        # Default: show the latest party
        display_party(parties[-1])
    elif args.rewind == -1:
        # Interactive: pick from a list
        print()
        print("  === Cat Party Time Machine ===")
        print()
        for i, p in enumerate(parties):
            name = os.path.basename(p).replace(".txt", "")
            label = "(oldest)" if i == 0 else "(latest)" if i == len(parties) - 1 else ""
            print(f"    {i}: {name}  {label}")
        print()
        try:
            choice = int(input("  Pick a party number: "))
        except (ValueError, EOFError):
            print("  Never mind then. Bye!")
            return
        if 0 <= choice < len(parties):
            display_party(parties[choice])
        else:
            print(f"  No party #{choice}. We only have {len(parties)}.")
    else:
        # Specific index
        idx = args.rewind
        if 0 <= idx < len(parties):
            display_party(parties[idx])
        else:
            print(f"No party #{idx}. We have {len(parties)} parties (0-{len(parties)-1}).")
            sys.exit(1)


if __name__ == "__main__":
    main()
