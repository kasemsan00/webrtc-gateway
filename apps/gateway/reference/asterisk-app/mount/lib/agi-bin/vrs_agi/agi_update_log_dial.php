#!/usr/bin/php -q
<?php
require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

/*  
 *  AGI ARGV Format
 * (dial_status, dial_duration)
 *  EX : (${DIALSTATUS}, ${ANSWEREDTIME}
 */

$request = array();
$agi = new AGI();

$ld_uniqueid = $agi->request['agi_uniqueid'];
$dial_status = $argv[1];
$dial_duration = intval($argv[2]);

$is_record_voice = $agi->get_variable('IS_RECORD_VOICE');
$is_voice = boolval($is_record_voice['data'])?'1':'0';

$strSQL = "UPDATE
                `log_dial`
            SET
                `dial_duration` = {$dial_duration},
                `dial_status` = '{$dial_status}',
                `last_update` = NOW(), 
                `is_voice` = '{$is_voice}'
            WHERE
                `ld_uniqueid` = '{$ld_uniqueid}';";

$agi->verbose($strSQL);

dbquery($strSQL);

return 0;
?>
