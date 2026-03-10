#!/usr/bin/php -q
<?php

require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

$source_number = $argv[1];

$strSQL = "INSERT INTO `block_by_system`(
                `source`,
                `last_update`
            )
            VALUES(
                '{$source_number}',
                NOW()
            )";

dbquery($strSQL);

return 0;
?>
