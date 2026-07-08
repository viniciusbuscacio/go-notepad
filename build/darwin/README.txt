go-Notepad for macOS
=================

Problem: The app does not open — "Apple could not verify go-Notepad is free of malware."

Answer: run this command once in Terminal, then open the app normally.
In the folder where you extracted the zip (or on the .app you already moved
to Applications):

    xattr -dr com.apple.quarantine go-notepad.app


Project & source
----------------
https://github.com/viniciusbuscacio/go-notepad
