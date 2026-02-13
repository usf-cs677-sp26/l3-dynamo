# File Transfer

## Changes Made

Our team did not modify the original `.proto` file. The current message
structure is already sufficient for this lab, so most of our changes were made
in the code rather than in the protocol itself.

The main changes are:

1. The server now checks whether there is enough available disk space before
   receiving a file. If space is insufficient, the upload is rejected.
2. On the client side, if the target path does not exist, the client creates
   the required directories and file first. If the server does not accept the
   transfer, the client removes the created file.

These updates were focused on handling failure cases more safely.

## Personal Perspective

For a file-transfer client/server system like this, the protocol should stay as
simple as possible. As long as errors are clearly reported and logged, the more
important part is implementing robust error handling in the code.
