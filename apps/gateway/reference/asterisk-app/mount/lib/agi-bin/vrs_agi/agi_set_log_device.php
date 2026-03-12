#!/usr/bin/php -q
<?php
/*
* AGI ARGV Format
* (destination_number, call_type, source_type, priority_type, group_type)
* Ex: (${EXTEN}, 1, 1, 2, 1)
*
* 
* Update 2/10/2019
*/

require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

$request = array();
$agi = new AGI();


$ls_uniqueid = $agi->request['agi_uniqueid'];
$peer = $agi -> get_variable("CHANNEL(peername)");
$exten = $agi -> get_variable("CHANNEL(exten)");
$context = $agi -> get_variable("CHANNEL(context)");
$channel = $agi -> get_variable("CHANNEL");
$ipaddress = $agi -> get_variable("CHANNEL(peerip)");
$port = $agi -> get_variable("CHANNEL(recvport)");
$useragent = $agi -> get_variable("CHANNEL(useragent)");
$audio_codec = $agi -> get_variable("CHANNEL(audionativeformat)");
$video_codec = $agi -> get_variable("CHANNEL(videonativeformat)");
$video_codec = $agi -> get_variable("CHANNEL(videonativeformat)");
$rtpqos = $agi -> get_variable("CHANNEL(rtpqos)");

$strSQL = "INSERT INTO `log_devices`(
                        `ls_uniqueid`,
                        `peer`,
                        `exten`,
                        `context`,
                        `channel`,
                        `ipaddress`,
                        `port`,
                        `useragent`,
                        `audio_codec`,
                        `video_codec`,
                        `rtpqos`,
                        `input_date`
                    )
                    VALUES('{$ls_uniqueid}',
                        '{$peer['data']}',
                        '{$exten['data']}',
                        '{$context['data']}',
                        '{$channel['data']}',
                        '{$ipaddress['data']}',
                        '{$port['data']}',
                        '{$useragent['data']}',
                        '{$audio_codec['data']}',
                        '{$video_codec['data']}',
                        '{$rtpqos['data']}',
                         NOW())";

dbquery($strSQL);

return 0;
?>
