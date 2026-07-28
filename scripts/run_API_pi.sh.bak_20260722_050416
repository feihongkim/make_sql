#!/bin/bash
docker rm -f API_pi

docker run -d -it --name API_pi   --cap-drop=ALL   --security-opt=no-new-privileges   --memory=4g --cpus=2   --network pinet   -p 3001:3001   -v /home/feihong/code/ContainerSetup/PiExtensions/telegram:/home/node/.pi/agent/extensions/telegram:ro   -v /home/feihong/code/ContainerSetup/PiExtensions/api_server:/home/node/.pi/agent/extensions/api_server:ro   -v /home/feihong/code/ContainerSetup/Tokens/API_pi.token:/home/node/.pi/agent/telegram_token:ro   -v /home/feihong/code/ContainerSetup/pi_keys.json:/home/node/.pi/agent/credentials:ro   -v /home/feihong/code/API:/workspace:rw   -v /home/feihong/code/Jarvis/project/API:/docs:rw   -v API_pi_data:/home/node   api_pi_2:12   pi --provider deepseek --model deepseek-v4-pro

echo "API_pi created"
