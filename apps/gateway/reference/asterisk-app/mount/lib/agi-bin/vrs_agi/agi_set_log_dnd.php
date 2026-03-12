#!/usr/bin/php -q
<?php
/*
* AGI ARGV Format
* ($agent_number, $dnd_type)
* Ex: 
*
* dnd_type : DND_1, DND_2, DND_OFF
* 
* Update 4/10/2019
*/

require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

$request = array();
$agi = new AGI();

$ldd_uniqueid = $agi->request['agi_uniqueid'];
$agent_number = $argv[1];
$dnd_type = $argv[2];

$strSQL = "INSERT INTO `log_dnd`(
                `ldd_uniqueid`,
                `agent`,
                `dnd_type`,
                `last_update`
            )
            VALUES(
                '{$ldd_uniqueid}',
                '{$agent_number}',
                '{$dnd_type}',
                NOW()
            )";

dbquery($strSQL);

return 0;
?>
