# Oarsman: A WaterRower S4 Interface

Simple interface to the WaterRower S4 monitor (USB version)

This is a very early stage hobbyist program to interface with the
WaterRower S4 monitor through the USB port. To be able to read from
the S4, the Prolific PL2303 USB to serial adapter driver may need to
be installed (OS specific). If required, there are versions for Mac
and Windows on Prolific's site.

The available commands are:

    version                   Print the version number
    init                      Initialize the database
    workout                   Start a rowing workout
    export                    Export workout data from database
    import                    Import workout data from database
    help [command]            Help about any command
 
The program uses a SQLite3 database to store metadata about the
workout activities. The database file and all raw workout logs are
stored under the folder `.oarsman` in the user's home directory. In
order to initialize the database for the first-time, type:

    $ oarsman init

Do a short 200m test workout:

    $ oarsman workout --distance=200

... now ... get rowing. Once done, come back to your computer and
CTRL+C (unfortunately that's the best I can do right now).

You can now export as TCX (Garmin Training Center). To find out the
workout activity id, list all available workouts using the `export`
command without and `id`:

    $ oarsman export
    id,start_time,distance,ave_speed,max_speed
    1404553035100,2014-07-05T09:37:15Z,16408,4.214744,5.950000
    1414596607600,2014-10-29T15:30:07Z,200,3.174603,5.600000

We see the id for the 200m workout we just did is `1414596607600`,
therefore we type:

    $ oarsman export --id=1414596607600
    2014/10/29 17:29:40 Writing aggregate data to /var/folders/qv/g537wtg1543clytlpl0xn_tm0000gn/T/com.olympum.Oarsman/1414596607600.tcx

You'll find the tcx file in that folder.

Note that the program captures rowing data (distance, stroke rate,
heart rate, etc.) every 25 ms, but the export is every 100ms. In fact,
although we create track points in the TCX file every 100ms, the
resolution in the official schema is 1,000ms, so depending on the
program you use data might be truncated, averaged, ... In general,
this should be okay.
