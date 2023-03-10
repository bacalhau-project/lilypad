// SPDX-License-Identifier: MIT
pragma solidity >=0.8.4;
import "lilypad/contracts/LilypadEvents.sol";
import "lilypad/contracts/LilypadCallerInterface.sol";
import "@openzeppelin/contracts/utils/Strings.sol";

contract DrandCaller is LilypadCallerInterface {
    LilypadEvents bridge;
    event RandomNumberGenerated(uint id, uint256 number);

    constructor(address bridgeContract) {
        bridge = LilypadEvents(bridgeContract);
    }

    string constant specStart =
        '{"Engine":"Docker","Verifier":"Noop","Publisher":"Estuary",'
        '"Docker":{"Image":"ghcr.io/bacalhau-project/drand-example:latest@sha256:a0c05006951268e23db468b02875d9a4388b9b0c10677801b7b07a1ce4dd3e80",'
        '"Entrypoint":["/bin/rand","';

    string constant specEnd =
        '"]},'
        '"inputs":[{"StorageSource":"URLDownload","URL":"https://api.drand.sh/8990e7a9aaed2ffed73dbd7092123d6f289930540d7651336225dc172e51b2ce/public/latest","path":"/inputs"}],"Deal":{"Concurrency":1}}';

    function getRandomNumber(uint256 minimum, uint256 maximum) external {
        string memory minHex = Strings.toHexString(minimum);
        string memory maxHex = Strings.toHexString(maximum);
        string memory spec = string.concat(specStart, minHex, '","', maxHex, specEnd);
        bridge.runBacalhauJob(address(this), spec, LilypadResultType.StdOut);
    }

    function hexStringToUint256(
        string calldata s
    ) internal pure returns (uint256) {
        bytes calldata b = bytes(s);
        require(b.length <= 64);
        require(b.length > 0);

        uint256 result = 0;
        for (uint8 i = 0; i < b.length; i++) {
            result <<= 4;
            result += uint8(b[i]);
            if (b[i] >= "A" && b[i] <= "F") {
                result -= 0x41 - 10;
            } else if (b[i] >= "a" && b[i] <= "f") {
                result -= 0x61 - 10;
            } else if (b[i] >= "0" && b[i] <= "9") {
                result -= 0x30;
            } else {
                revert("invalid character in hex string");
            }
        }

        return result;
    }

    function lilypadFulfilled(
        address _from,
        uint _jobId,
        LilypadResultType _resultType,
        string calldata _result
    ) external override {
        require(_resultType == LilypadResultType.StdOut);
        require(_from == address(bridge));
        emit RandomNumberGenerated(_jobId, hexStringToUint256(_result));
    }

    function lilypadCancelled(
        address _from,
        uint,
        string calldata _errorMsg
    ) external override view {
        require(_from == address(bridge));
        console.log(_errorMsg);
    }
}
