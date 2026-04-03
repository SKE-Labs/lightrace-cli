"""Lightrace CLI — thin Python wrapper that executes the Go binary."""

import os
import sys
import platform as _platform
from pathlib import Path


def _get_binary_path() -> Path:
    """Locate the lightrace binary bundled in this wheel."""
    # The binary is placed in the package's data/scripts directory by the wheel
    pkg_dir = Path(__file__).parent
    binary_name = "lightrace.exe" if sys.platform == "win32" else "lightrace"

    # Check next to this file (wheel installs binary here)
    candidate = pkg_dir / binary_name
    if candidate.exists():
        return candidate

    # Check in the scripts dir (pip installs entry_points there)
    for scripts_dir in [
        Path(sys.prefix) / "bin",
        Path(sys.prefix) / "Scripts",
    ]:
        candidate = scripts_dir / binary_name
        if candidate.exists():
            return candidate

    raise FileNotFoundError(
        f"Could not find lightrace binary. "
        f"Platform: {sys.platform} {_platform.machine()}"
    )


def main():
    """Entry point — exec the Go binary with sys.argv."""
    binary = _get_binary_path()
    os.execvp(str(binary), [str(binary)] + sys.argv[1:])
