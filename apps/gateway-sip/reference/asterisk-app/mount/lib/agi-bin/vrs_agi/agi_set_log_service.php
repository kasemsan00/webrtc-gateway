#!/usr/bin/php -q
<?php
/*
* AGI ARGV Format
* (destination_number, call_type, source_type, priority_type, group_type)
* Ex: (${EXTEN}, 1, 1, 2, 1)
*
* call_type : 1=MAKING, 2=RECEIVING, 3=VRI
* source_type : 1=VP, 2=KIOSK, 3=MOBILE, 4=WEB, 5=CAPTION
* priority_type : 1=NORMAL, 2=EMG
* group_type : 1=SERVICE, 2=HELPDESK, 3=TECHNICIAN, 4=TEST
* 
* Update 2/10/2019
*/

require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

$request = array();
$agi = new AGI();

$ls_uniqueid = $agi->request['agi_uniqueid'];
$source_number = $agi->request['agi_callerid'];
$source_name = $agi->request['agi_calleridname'];
$extension = $agi->request['agi_extension'];

$destination_number = $argv[1];
$call_type = $argv[2];
$source_type = $argv[3];
$priority = $argv[4];
$group = $argv[5];

$agi->set_variable('__USER_CALL_TYPE', $call_type);
$agi->set_variable('__USER_SOURCE_TYPE', $source_type);
$agi->set_variable('__USER_PRIORITY_TYPE', $priority);
$agi->set_variable('__USER_GROUP_TYPE', $group);
$agi->set_variable('__USER_OUT_MAPPING', $destination_number);
$agi->set_variable('__USER_UNIQUEID', $ls_uniqueid);
$agi->set_variable('__USER_ID', $source_number);
$agi->set_variable('__USER_NAME', $source_name);
$agi->set_variable('__USER_EXTEN', $extension);

$destination_number = !empty($destination_number)?'\''.$destination_number.'\'':'NULL';

/* clear timeout pending */
$idcard_del_pending = "DELETE
                        FROM
                            `idcard_pending`
                        WHERE
                            TIMESTAMPDIFF(MINUTE, `input_date`, NOW()) > 5";
dbquery($idcard_del_pending);

$idcard_select_pending = "SELECT
                                `idcard`
                            FROM
                                `idcard_pending`
                            WHERE
                                `source` = '{$source_number}'
                            ORDER BY
                                `idcard_pending`.`pending_id`
                            DESC
                            LIMIT 1";
$idcard_result = dbquery($idcard_select_pending, DB_FETCH_ROW);
if(!empty($idcard_result)){
    $idcard = '\''.$idcard_result['idcard'].'\'';
    $idcard_del_pending = "DELETE
                            FROM
                                `idcard_pending`
                            WHERE
                                `source` = '{$source_number}'";
    dbquery($idcard_del_pending);
}else $idcard = 'NULL';

$tableName = strcmp($source_type, "TEXT") === 0 ? 'log_service_text' : 'log_service';

$strSQL = "INSERT INTO `{$tableName}`(
                `ls_uniqueid`,
                `source`,
                `destination`,
                `idcard`,
                `call_type`,
                `source_type`,
                `priority`,
                `group`
            )
            VALUES(
                '{$ls_uniqueid}',
                '{$source_number}',
                {$destination_number},
                {$idcard},
                '{$call_type}',
                '{$source_type}',
                '{$priority}',
                '{$group}'
            )";

dbquery($strSQL);

return 0;
?>
