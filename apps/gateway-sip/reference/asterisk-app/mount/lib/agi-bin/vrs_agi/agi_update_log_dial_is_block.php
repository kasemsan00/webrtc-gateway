#!/usr/bin/php -q
<?php
require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

/*
* ARGV Fomat example
* 
* AGI(${VRS_AGI_DIR}/agi_update_log_dial_is_block.php
*/

$request = array();
$agi = new AGI();

$ld_uniqueid = $agi->request['agi_uniqueid'];

$strSQL = "UPDATE
                `log_dial`
            SET
                `is_block` = '1'
            WHERE
                `ld_uniqueid` = '{$ld_uniqueid}';";

$agi->verbose($strSQL);

dbquery($strSQL);

return 0;
?>