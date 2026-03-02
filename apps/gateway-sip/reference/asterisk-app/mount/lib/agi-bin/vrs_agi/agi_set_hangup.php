#!/usr/bin/php -q
<?php
require_once 'lib/phpagi.php';

$agi = new AGI();
$channal_id = $agi->request['agi_channel'];
$number = $agi->request['agi_callerid'];

$strChannels = sprintf("/usr/sbin/rasterisk -x 'core show channels concise'");
exec($strChannels, $resultChannels);
if (empty($resultChannels)) {
	return 1;
}

foreach ($resultChannels as $row) {
	if (empty($row) || !strstr($row, '!')) {
		continue;
	}
	$row = explode('!', $row);
	if($row[7] == $number){
		if($row[0] != $channal_id && $row[4] != "Ringing"){
			$agi->verbose("Kill channel ".$row[0]);
			$killChannels = sprintf("/usr/sbin/rasterisk -x 'channel request hangup {$row[0]}'");
			exec($killChannels, $resultkill);
			
		}
	}
}



?>
