<?php

require dirname(__DIR__) . '/vendor/autoload.php';

use ZephraLune\MoireAtlas\ZephraluneMoireSurveyor;

$zephraluneSurveyor = new ZephraluneMoireSurveyor();
print $zephraluneSurveyor->renderZephraluneDigest();
