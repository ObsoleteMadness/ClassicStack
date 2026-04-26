tell application "Finder"
    activate

    set results to "Test Started\r"

    try
        -- Placeholder for mounting and testing
        set results to results & "Test Completed\r"
    on error
        set results to results & "Test Failed\r"
    end try

    try
        set theFile to open for access file "TestDisk:results.txt" with write permission
        write results to theFile
        close access theFile
    on error
        try
            close access file "TestDisk:results.txt"
        end try
    end try
end tell
