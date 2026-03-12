#!/usr/bin/php -q
<?php
require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

$request = array();
$agi = new AGI();

$incoming_source = $agi->request['agi_callerid'];

$strSQL = "SELECT
			`source`,
			`is_block`
			FROM
			`block_list`
			WHERE
			`source` IN('{$incoming_source}');";

$row = dbquery($strSQL, DB_FETCH_ROW);

if (!empty($row)) {
	if($row['source'] == $incoming_source && $row['is_block'] == '1'){
		$agi->verbose("Block source number : {$incoming_source}");
		$agi->hangup();
	}
		
}
return 0;
?>

