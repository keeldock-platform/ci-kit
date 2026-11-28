# Fixed lock-generation tools

Fixed Linux x64 qualification and staging only; no source checkout, lifecycle, credentials or arbitrary command input. `release-lock.json` pins public metadata. `qualified.json` records the reviewed native Linux production qualification from CI policy run 46090313643, attempt 1, artifact 15240892985; staging requires its exact script, release-lock and complete tool hashes.

The trusted Go process runner owns deadlines and descendant cleanup. `stage.py` bounds capture and archive reads before parsing. The production policy run passed all 38 source-free staging fixtures. This tool qualification does not grant source access: dependency-lock return also requires the explicit Kit purpose gate and the independently trusted per-candidate owner policy.
