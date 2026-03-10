#!/usr/bin/php -q
<?php
require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

/*  
 *  AGI ARGV Format
 * (source_number, ls_uniqueid)
 *  EX : (${USER_CALL},${USER_UNIQUEID})
 */

$request = array();
$agi = new AGI();

$type = $argv[1];
$source_number = $argv[1];
$ls_uniqueid = !empty($argv[2])?'\''.$argv[2].'\'':'NULL';

$ld_uniqueid = $agi->request['agi_uniqueid'];
$destination_number = $agi->request['agi_extension'];
$is_external_dial = $agi->get_variable('IS_EXTERNAL_DIAL');

$provider = boolval($is_external_dial['data'])?'\''.$agi->request['agi_callerid'].'\'':'NULL';

$strSQL = "INSERT INTO `log_dial`
            (
                `ld_uniqueid`,
                `source`,
                `destination`,
                `provider`,
                `last_update`,
                `ls_uniqueid`
            ) 
            VALUES 
            (
                '{$ld_uniqueid}',
                '{$source_number}',
                '{$destination_number}',
                {$provider}, 
                NOW(), 
                {$ls_uniqueid}
            )";

dbquery($strSQL);

return 0;
?>
